//go:build linux

package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// confPath ist die uebliche Ablage fuer Dienstkonfiguration unter Linux.
// Das Paket (.deb/.rpm) legt sie an, systemd liest sie per EnvironmentFile.
const confPath = "/etc/itop-agent.conf"

func path() string {
	if p := os.Getenv("ITOP_AGENT_CONF"); p != "" {
		return p
	}
	return confPath
}

// fromStore liest eine Zeile der Form SCHLUESSEL=wert.
//
// Bewusst dasselbe Format wie systemds EnvironmentFile: so laesst sich die
// Datei ohne Umweg in die Unit einhaengen, und wer sie von Hand anfasst, muss
// keine zweite Syntax lernen.
func fromStore(key string) string {
	f, err := os.Open(path())
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		return strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return ""
}

// toStore wird unter Linux nicht gebraucht - die Datei kommt aus dem Paket.
func toStore(key, value string) error {
	dir := filepath.Dir(path())
	if _, err := os.Stat(dir); err != nil {
		return err
	}
	// Bewusst nicht implementiert: die Konfiguration gehoert zum Paket, nicht
	// zum Programm. Ein Agent, der sich selbst konfiguriert, umgeht Ansible und
	// die Paketverwaltung.
	return os.ErrPermission
}
