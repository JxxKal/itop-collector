package collect

import "testing"

func TestSanitizeSerial(t *testing.T) {
	leer := []string{
		"", "  ", "0", "None", "Default string", "To Be Filled By O.E.M.",
		"System Serial Number", "Not Specified", "not applicable", "123456789",
		"0000000", "XXXXXXXX", "n/a",
	}
	for _, in := range leer {
		if got := SanitizeSerial(in); got != "" {
			t.Errorf("SanitizeSerial(%q) = %q, erwartet leer", in, got)
		}
	}
	echt := map[string]string{
		"  SN-ALPHA-001 ": "SN-ALPHA-001",
		"5CG1234ABC":      "5CG1234ABC",
		"VMware-42 0a bc": "VMware-42 0a bc",
		"AB":              "AB", // zu kurz fuer die Wiederholungsregel
	}
	for in, want := range echt {
		if got := SanitizeSerial(in); got != want {
			t.Errorf("SanitizeSerial(%q) = %q, erwartet %q", in, got, want)
		}
	}
}
