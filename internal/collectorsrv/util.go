package collectorsrv

import "crypto/subtle"

// constantEquals vergleicht zwei Zeichenketten in konstanter Zeit.
//
// Fuer Tokenvergleiche: ein normales == bricht beim ersten abweichenden Byte ab
// und verraet ueber die Laufzeit, wie viele Zeichen stimmen.
func constantEquals(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
