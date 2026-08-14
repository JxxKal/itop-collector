//go:build windows

package collect

import (
	"strings"

	"github.com/JxxKal/itop-collector/internal/report"
	"golang.org/x/sys/windows/registry"
)

// uninstallKeys sind die Orte, an denen Windows installierte Programme fuehrt.
//
// WOW6432Node MUSS mit: auf 64-Bit-Windows landen 32-Bit-Programme
// ausschliesslich dort. Ohne diesen Pfad fehlt regelmaessig die Haelfte des
// Inventars, ohne dass etwas auf einen Fehler hindeutet.
//
// Die Benutzer-Hive (HKCU) bleibt bewusst aussen vor: der Agent laeuft als
// Dienst unter SYSTEM und saehe dort nur dessen eigenes, leeres Profil.
var uninstallKeys = []struct {
	root registry.Key
	path string
}{
	{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
}

// software liest das Programminventar aus der Registry.
//
// AUSDRUECKLICH NICHT ueber Win32_Product: diese WMI-Klasse loest bei JEDER
// Abfrage eine Konsistenzpruefung samt Reparatur aller MSI-Pakete aus. Das
// dauert Minuten, schreibt Ereignisprotokoll-Eintraege fuer jedes Produkt und
// kann laufende Installationen stoeren. Die Registry liefert dieselbe Auskunft
// in Millisekunden.
func software() []report.Software {
	seen := map[string]bool{}
	var out []report.Software

	for _, k := range uninstallKeys {
		key, err := registry.OpenKey(k.root, k.path, registry.READ)
		if err != nil {
			// Auf 32-Bit-Windows gibt es WOW6432Node nicht - das ist kein Fehler.
			continue
		}
		names, err := key.ReadSubKeyNames(-1)
		if err != nil {
			key.Close()
			continue
		}
		for _, name := range names {
			sub, err := registry.OpenKey(key, name, registry.READ)
			if err != nil {
				continue
			}
			sw, ok := readEntry(sub)
			sub.Close()
			if !ok {
				continue
			}
			// Dieselbe Software steht oft in beiden Hives. Ueber Name+Version
			// entdoppeln, sonst erscheint jedes 32-Bit-Programm zweimal.
			key := sw.Name + "\x00" + sw.Version
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, sw)
		}
		key.Close()
	}
	return out
}

// readEntry liest einen Uninstall-Eintrag und entscheidet, ob er zaehlt.
func readEntry(k registry.Key) (report.Software, bool) {
	name, _, err := k.GetStringValue("DisplayName")
	if err != nil || strings.TrimSpace(name) == "" {
		// Eintraege ohne Anzeigenamen sind Fragmente - die zeigt auch die
		// Systemsteuerung nicht an.
		return report.Software{}, false
	}
	// SystemComponent=1 markiert Laufzeitbibliotheken und Update-Reste, die
	// Windows selbst ausblendet. Sie wuerden das Inventar mit Hunderten von
	// Eintraegen fluten, die niemanden interessieren.
	if v, _, err := k.GetIntegerValue("SystemComponent"); err == nil && v == 1 {
		return report.Software{}, false
	}
	// Ein gesetzter ParentKeyName kennzeichnet Updates zu einem Hauptprodukt
	// (typisch Sicherheitsupdates). Das Produkt selbst steht separat.
	if parent, _, err := k.GetStringValue("ParentKeyName"); err == nil && parent != "" {
		return report.Software{}, false
	}

	version, _, _ := k.GetStringValue("DisplayVersion")
	publisher, _, _ := k.GetStringValue("Publisher")
	return report.Software{
		Name:      strings.TrimSpace(name),
		Version:   strings.TrimSpace(version),
		Publisher: strings.TrimSpace(publisher),
	}, true
}
