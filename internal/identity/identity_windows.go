//go:build windows

package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// regPath haelt GUID und Device-Token.
//
// HKLM statt HKCU: der Agent laeuft als Dienst unter SYSTEM, und die Kennung
// muss unabhaengig vom angemeldeten Benutzer gelten. Die Standard-ACL auf
// HKLM\SOFTWARE erlaubt Schreibzugriff nur SYSTEM und Administratoren - genau
// das ist gewuenscht, weil das Device-Token zum Melden im Namen dieser Maschine
// berechtigt.
const regPath = `SOFTWARE\iTopAgent`

// regPathEnv erlaubt es, den Schluessel zu verlegen (Tests, Sonderfaelle).
const regPathEnv = "ITOP_AGENT_REG_PATH"

func path() string {
	if p := os.Getenv(regPathEnv); p != "" {
		return p
	}
	return regPath
}

// AgentGUID liefert die dauerhafte Kennung und legt sie beim ersten Aufruf an.
//
// Die GUID ueberlebt Umbenennungen der Maschine - nicht aber ein Reimaging, weil
// dabei die Registry verschwindet. Genau so ist es gewollt; den Reimaging-Fall
// loest der Collector ueber die Seriennummer auf.
func AgentGUID() (string, error) {
	if guid, err := readValue("AgentGuid"); err == nil && guid != "" {
		return guid, nil
	}
	guid, err := newUUIDv4()
	if err != nil {
		return "", err
	}
	if err := writeValue("AgentGuid", guid); err != nil {
		return "", err
	}
	return guid, nil
}

// DeviceToken liest das beim Enrollment erhaltene Token.
func DeviceToken() (string, error) {
	v, err := readValue("DeviceToken")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", fmt.Errorf("kein Device-Token hinterlegt")
	}
	return v, nil
}

// SaveDeviceToken legt das Token ab.
func SaveDeviceToken(token string) error {
	return writeValue("DeviceToken", token)
}

func readValue(name string) (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path(), registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("Schluessel HKLM\\%s oeffnen: %w", path(), err)
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return "", fmt.Errorf("Wert %s lesen: %w", name, err)
	}
	return strings.TrimSpace(v), nil
}

func writeValue(name, value string) error {
	// CreateKey legt den Schluessel an oder oeffnet ihn, falls vorhanden.
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, path(), registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("Schluessel HKLM\\%s anlegen: %w (Administratorrechte noetig)", path(), err)
	}
	defer k.Close()
	if err := k.SetStringValue(name, value); err != nil {
		return fmt.Errorf("Wert %s schreiben: %w", name, err)
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
