// Package config liest die Agent-Konfiguration.
//
// Reihenfolge: Umgebungsvariable zuerst, dann der plattformeigene Speicher
// (Windows-Registry bzw. /etc/itop-agent.conf).
//
// Warum ueberhaupt ein zweiter Ort: ein Windows-Dienst erbt die
// Umgebungsvariablen der aufrufenden Shell NICHT. Wer den Agent interaktiv mit
// gesetztem ITOP_COLLECTOR_URL testet und ihn dann als Dienst installiert,
// bekommt einen Dienst, der beim Start sofort aussteigt. Der plattformeigene
// Speicher ist der Ort, den auch der Installer (MSI/GPO bzw. .deb/.rpm) befuellt.
package config

import "os"

// Schluesselnamen. Als Umgebungsvariable in Grossschreibung mit Praefix, im
// plattformeigenen Speicher ohne.
const (
	KeyCollectorURL  = "ITOP_COLLECTOR_URL"
	KeyCACert        = "ITOP_CA_CERT"
	KeySkipTLSVerify = "ITOP_SKIP_TLS_VERIFY"

	// KeyEnrollToken haelt das Einmal-Token, bis der Agent sich damit
	// registriert hat. Danach loescht er es - es ist einmalig und hat im
	// Dauerbetrieb nichts mehr auf dem Geraet zu suchen.
	//
	// Der Installer (MSI, GPO, Ansible) hinterlegt es hier, statt es dem Agent
	// als Argument mitzugeben. So laesst sich unbeaufsichtigt ausrollen, ohne
	// dass das Token in Prozesslisten oder Skripten auftaucht.
	KeyEnrollToken = "ITOP_ENROLL_TOKEN"
)

// Get liefert einen Konfigurationswert oder den Vorgabewert.
func Get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if v := fromStore(key); v != "" {
		return v
	}
	return def
}

// Set schreibt einen Wert in den plattformeigenen Speicher.
func Set(key, value string) error { return toStore(key, value) }

// Delete entfernt einen Wert aus dem plattformeigenen Speicher.
//
// Gebraucht fuer das Einmal-Token nach erfolgreicher Registrierung. Fehlt der
// Wert bereits, ist das kein Fehler.
func Delete(key string) error { return deleteFromStore(key) }
