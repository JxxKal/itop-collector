//go:build linux

package collect

import (
	"os/exec"
	"strings"
	"time"

	"github.com/JxxKal/itop-collector/internal/report"
)

// software liest das Paketinventar ueber den vorhandenen Paketmanager.
//
// Bewusst ueber die Kommandozeilenwerkzeuge und nicht ueber die Datenbanken
// direkt: deren Formate sind versionsabhaengig, die Ausgabeformate von
// dpkg-query und rpm sind seit Jahren stabil. Der Agent bleibt damit ohne
// externe Go-Abhaengigkeiten.
func software() []report.Software {
	if path, err := exec.LookPath("dpkg-query"); err == nil {
		return runPackageQuery(path,
			"-W", "-f=${Package}\t${Version}\t${Maintainer}\n")
	}
	if path, err := exec.LookPath("rpm"); err == nil {
		return runPackageQuery(path,
			"-qa", "--qf=%{NAME}\t%{VERSION}-%{RELEASE}\t%{VENDOR}\n")
	}
	return nil
}

// runPackageQuery fuehrt die Abfrage mit Zeitlimit aus.
//
// Zeitlimit, weil ein haengender Paketmanager (Lock durch ein laufendes
// Update, kaputte Datenbank) sonst den ganzen Sammellauf blockieren wuerde.
// Der Agent darf nie stehenbleiben - im Zweifel meldet er ohne Softwareliste.
func runPackageQuery(path string, args ...string) []report.Software {
	cmd := exec.Command(path, args...)
	done := make(chan []byte, 1)
	go func() {
		out, err := cmd.Output()
		if err != nil {
			done <- nil
			return
		}
		done <- out
	}()

	var out []byte
	select {
	case out = <-done:
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		return nil
	}
	if out == nil {
		return nil
	}

	lines := strings.Split(string(out), "\n")
	pkgs := make([]report.Software, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		sw := report.Software{Name: parts[0], Version: parts[1]}
		if len(parts) > 2 {
			sw.Publisher = strings.TrimSpace(parts[2])
		}
		pkgs = append(pkgs, sw)
	}
	return pkgs
}
