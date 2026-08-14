package collectorsrv

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Device ist ein registriertes Geraet.
//
// Der Collector ist bewusst zustandsarm: hier steht NUR, was iTop nicht wissen
// kann - der Token-Hash und die einmal festgelegte Zielklasse. Inventardaten
// liegen ausschliesslich in iTop.
type Device struct {
	AgentGUID   string      `json:"agent_guid"`
	TokenHash   string      `json:"token_hash"` // sha256 hex, nie das Token selbst
	TargetClass TargetClass `json:"target_class"`
	EnrolledAt  time.Time   `json:"enrolled_at"`
	LastSeen    time.Time   `json:"last_seen"`
	Blocked     bool        `json:"blocked"`
}

// Registry haelt die Geraete-Zuordnung.
//
// Ablage als eine JSON-Datei, nicht SQLite: der Datenbestand ist klein (eine
// Zeile pro Geraet), wird selten geschrieben und soll ohne Werkzeug lesbar und
// sicherbar sein. Bei einer Flotte in der Groessenordnung Zehntausende waere
// SQLite die bessere Wahl - dann ist nur diese Datei zu ersetzen.
type Registry struct {
	path string
	mu   sync.RWMutex
	devs map[string]*Device
}

// ErrUnknownDevice meldet ein nicht registriertes oder gesperrtes Geraet.
var ErrUnknownDevice = errors.New("Geraet unbekannt oder gesperrt")

// LoadRegistry liest die Registry oder legt eine leere an.
func LoadRegistry(path string) (*Registry, error) {
	r := &Registry{path: path, devs: map[string]*Device{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Registry lesen: %w", err)
	}
	var list []*Device
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("Registry auswerten: %w", err)
	}
	for _, d := range list {
		r.devs[d.AgentGUID] = d
	}
	return r, nil
}

// save schreibt die Registry atomar: erst in eine Nachbardatei, dann umbenennen.
// Ein Absturz mitten im Schreiben darf die Registry nicht zerstoeren - sie ist
// der einzige Zustand, den der Collector hat.
func (r *Registry) save() error {
	list := make([]*Device, 0, len(r.devs))
	for _, d := range r.devs {
		list = append(list, d)
	}
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("Registry serialisieren: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return fmt.Errorf("Registry-Verzeichnis anlegen: %w", err)
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("Registry schreiben: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		return fmt.Errorf("Registry umbenennen: %w", err)
	}
	return nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Enroll registriert ein Geraet und gibt sein Device-Token zurueck.
//
// Das Token wird nur hier im Klartext gesehen; gespeichert wird ausschliesslich
// der Hash. Ein Angreifer mit Lesezugriff auf die Registry kann sich damit nicht
// als Geraet ausgeben.
func (r *Registry) Enroll(agentGUID string) (string, *Device, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("Token erzeugen: %w", err)
	}
	token := hex.EncodeToString(buf)

	r.mu.Lock()
	defer r.mu.Unlock()

	dev, ok := r.devs[agentGUID]
	if !ok {
		dev = &Device{AgentGUID: agentGUID, EnrolledAt: time.Now().UTC()}
		r.devs[agentGUID] = dev
	}
	// Erneutes Enrollment ersetzt das Token. Die einmal festgelegte Zielklasse
	// bleibt - sonst wuerde ein neu installierter Agent ein zweites CI erzeugen.
	dev.TokenHash = hashToken(token)
	dev.Blocked = false
	if err := r.save(); err != nil {
		return "", nil, err
	}
	return token, dev, nil
}

// Authenticate prueft ein Device-Token.
func (r *Registry) Authenticate(agentGUID, token string) (*Device, error) {
	r.mu.RLock()
	dev, ok := r.devs[agentGUID]
	r.mu.RUnlock()
	if !ok || dev.Blocked {
		return nil, ErrUnknownDevice
	}
	// Konstante Laufzeit, damit sich das Token nicht ueber Zeitmessung raten laesst.
	if subtle.ConstantTimeCompare([]byte(dev.TokenHash), []byte(hashToken(token))) != 1 {
		return nil, ErrUnknownDevice
	}
	return dev, nil
}

// Touch vermerkt eine erfolgreiche Meldung und sichert die Registry.
func (r *Registry) Touch(dev *Device) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dev.LastSeen = time.Now().UTC()
	return r.save()
}

// Count liefert die Anzahl registrierter Geraete.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.devs)
}
