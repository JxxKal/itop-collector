//go:build windows

package config

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// regPath ist derselbe Schluessel, unter dem auch GUID und Device-Token liegen.
// Ein Ort fuer alles, was den Agent betrifft - und die ACL von HKLM\SOFTWARE
// schuetzt ihn bereits.
const regPath = `SOFTWARE\iTopAgent`

// valueName bildet den Umgebungsvariablennamen auf den Registry-Wert ab.
//
// In der Registry ohne ITOP_-Praefix und in gemischter Schreibweise, weil dort
// der Kontext schon aus dem Schluesselnamen hervorgeht.
func valueName(key string) string {
	switch key {
	case KeyCollectorURL:
		return "CollectorUrl"
	case KeyCACert:
		return "CaCertPath"
	case KeySkipTLSVerify:
		return "SkipTlsVerify"
	case KeyEnrollToken:
		return "EnrollToken"
	}
	return key
}

func fromStore(key string) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regPath, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue(valueName(key))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

func deleteFromStore(key string) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regPath, registry.SET_VALUE)
	if err != nil {
		return nil // Schluessel gibt es nicht - nichts zu tun
	}
	defer k.Close()
	if err := k.DeleteValue(valueName(key)); err != nil &&
		!errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func toStore(key, value string) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, regPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(valueName(key), value)
}
