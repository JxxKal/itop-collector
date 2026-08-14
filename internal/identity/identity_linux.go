//go:build linux

// Package identity verwaltet die dauerhafte Kennung des Agents.
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stateDirEnv erlaubt es, die Ablage zu verlegen.
//
// Gebraucht fuer zwei Faelle: Tests ohne root-Rechte und Betrieb im Container,
// wo /var/lib/itop-agent ein Volume an anderer Stelle sein kann. Im Regelbetrieb
// nicht gesetzt - dann gilt der feste Pfad.
const stateDirEnv = "ITOP_AGENT_STATE_DIR"

func dir() string {
	if d := os.Getenv(stateDirEnv); d != "" {
		return d
	}
	return stateDir
}

// stateDir haelt GUID und Device-Token.
//
// 0700 auf dem Verzeichnis, 0600 auf den Dateien: das Device-Token berechtigt
// zum Melden im Namen dieser Maschine und darf fuer normale Benutzer nicht
// lesbar sein.
const stateDir = "/var/lib/itop-agent"

// AgentGUID liefert die dauerhafte Kennung und legt sie beim ersten Aufruf an.
//
// Die GUID ueberlebt Umbenennungen der Maschine - nicht aber ein Reimaging, weil
// dabei das Dateisystem verschwindet. Genau so ist es gewollt; den
// Reimaging-Fall loest der Collector ueber die Seriennummer auf.
func AgentGUID() (string, error) {
	path := filepath.Join(dir(), "agent.guid")
	if b, err := os.ReadFile(path); err == nil {
		if guid := strings.TrimSpace(string(b)); guid != "" {
			return guid, nil
		}
	}
	guid, err := newUUIDv4()
	if err != nil {
		return "", err
	}
	if err := writeSecret(path, guid); err != nil {
		return "", err
	}
	return guid, nil
}

// DeviceToken liest das beim Enrollment erhaltene Token.
func DeviceToken() (string, error) {
	b, err := os.ReadFile(filepath.Join(dir(), "device.token"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// SaveDeviceToken legt das Token ab.
func SaveDeviceToken(token string) error {
	return writeSecret(filepath.Join(dir(), "device.token"), token)
}

func writeSecret(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("Verzeichnis %s anlegen: %w", filepath.Dir(path), err)
	}
	// Erst schreiben, dann umbenennen: ein Absturz mitten im Schreiben darf
	// keine halbe GUID hinterlassen - die waere schlimmer als gar keine.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content+"\n"), 0o600); err != nil {
		return fmt.Errorf("%s schreiben: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("%s umbenennen: %w", path, err)
	}
	return nil
}

// newUUIDv4 erzeugt eine UUID nach RFC 4122, ohne externe Abhaengigkeit.
func newUUIDv4() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("Zufall lesen: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variante RFC 4122
	h := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]), nil
}
