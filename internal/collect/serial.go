package collect

import "strings"

// oemPlaceholders sind Werte, die Hersteller ins DMI schreiben, wenn sie das
// Feld nicht befuellt haben. Sie sehen aus wie Seriennummern, sind aber auf
// tausenden Geraeten identisch.
//
// Warum das wichtig ist: der Collector nutzt die Seriennummer, um nach einem
// Reimaging das vorhandene CI wiederzufinden. Ein Platzhalter wuerde alle
// Geraete desselben Modells zu einem einzigen CI verschmelzen - schlimmer als
// gar kein Abgleich. Deshalb wird hier zu LEER bereinigt, und der Collector
// sucht mit leeren Werten grundsaetzlich nicht.
//
// Liste erweiterbar; Vergleich ohne Beachtung von Gross-/Kleinschreibung und
// umgebenden Leerzeichen.
var oemPlaceholders = map[string]bool{
	"":                       true,
	"0":                      true,
	"none":                   true,
	"default string":         true,
	"to be filled by o.e.m.": true,
	"to be filled by o.e.m":  true,
	"system serial number":   true,
	"not specified":          true,
	"not applicable":         true,
	"123456789":              true,
	"invalid":                true,
	"unknown":                true,
	"n/a":                    true,
	"na":                     true,
	"xxxxxxx":                true,
	"empty":                  true,
	"chassis serial number":  true,
	"serial number":          true,
	"oem":                    true,
}

// SanitizeSerial liefert eine brauchbare Seriennummer oder den Leerstring.
func SanitizeSerial(raw string) string {
	s := strings.TrimSpace(raw)
	if oemPlaceholders[strings.ToLower(s)] {
		return ""
	}
	// Reine Wiederholungen eines Zeichens ("0000000", "XXXXXXXX") sind ebenfalls
	// Platzhalter, tauchen aber in zu vielen Laengen auf, um sie aufzulisten.
	if isRepeatedChar(s) {
		return ""
	}
	return s
}

func isRepeatedChar(s string) bool {
	if len(s) < 3 {
		return false
	}
	first := s[0]
	for i := 1; i < len(s); i++ {
		if s[i] != first {
			return false
		}
	}
	return true
}
