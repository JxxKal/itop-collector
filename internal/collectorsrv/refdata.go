package collectorsrv

import (
	"fmt"
	"strings"
	"sync"
)

// Fremdschluessel in iTop lassen sich nicht mit freiem Text fuellen. osfamily_id
// und osversion_id zeigen auf echte Objekte (OSFamily, OSVersion); der Agent
// meldet aber nur Zeichenketten wie "Windows 11 Pro" / "10.0.22631".
//
// Ohne Vorarbeit scheitert der Import mit einer Meldung, die nicht verraet,
// welches Feld gemeint ist:
//
//	Unable to create destination object: No result for the single row query
//
// Zwei Dinge sind noetig, beide sind hier bzw. in der Datasource erledigt:
//
//  1. In der Datasource steht an den Fremdschluesseln reconciliation_attcode =
//     "name", damit iTop den Namen statt einer id erwartet.
//  2. Das referenzierte Objekt muss existieren - dafuer sorgt EnsureOSRefs.

// refCache merkt sich bereits geprüfte Namen, damit nicht jede Meldung zwei
// zusaetzliche REST-Abfragen ausloest. Der Bestand aendert sich selten.
type refCache struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newRefCache() *refCache { return &refCache{seen: map[string]bool{}} }

func (c *refCache) has(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen[key]
}

func (c *refCache) add(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen[key] = true
}

// ensureByName legt ein Objekt an, falls es unter diesem Namen noch nicht existiert.
func (c *ITopClient) ensureByName(class, name string, extraFields map[string]any) error {
	res, err := c.rest(map[string]any{
		"operation":     "core/get",
		"class":         class,
		"key":           fmt.Sprintf("SELECT %s WHERE name = '%s'", class, oqlEscape(name)),
		"output_fields": "id,name",
	})
	if err != nil {
		return fmt.Errorf("%s %q suchen: %w", class, name, err)
	}
	if len(res.Objects) > 0 {
		return nil
	}
	fields := map[string]any{"name": name}
	for k, v := range extraFields {
		fields[k] = v
	}
	created, err := c.rest(map[string]any{
		"operation":     "core/create",
		"class":         class,
		"fields":        fields,
		"output_fields": "id,name",
		"comment":       "itop-agent collector: fehlender Referenzeintrag",
	})
	if err != nil {
		return fmt.Errorf("%s %q anlegen: %w", class, name, err)
	}
	for _, o := range created.Objects {
		if o.Code != 0 {
			return fmt.Errorf("%s %q anlegen: %s", class, name, o.Message)
		}
	}
	c.log.Info("Referenzeintrag angelegt", "klasse", class, "name", name)
	return nil
}

// EnsureOSRefs sorgt dafuer, dass OSFamily und OSVersion aus der Meldung als
// Objekte existieren.
//
// OSVersion haengt an OSFamily - die Familie wird deshalb zuerst angelegt und
// beim Anlegen der Version mitgegeben. Leere Werte werden uebersprungen: eine
// OSFamily namens "" waere ein Datenmuellhaufen, und der Agent darf laut
// Konvention jedes Feld leer lassen.
func (s *Service) EnsureOSRefs(osName, osVersion string) error {
	osName = strings.TrimSpace(osName)
	osVersion = strings.TrimSpace(osVersion)
	if osName == "" {
		return nil
	}
	if !s.refs.has("family:" + osName) {
		if err := s.itop.ensureByName("OSFamily", osName, nil); err != nil {
			return err
		}
		s.refs.add("family:" + osName)
	}
	if osVersion == "" {
		return nil
	}
	// OSVersion-Namen sind nur innerhalb einer Familie eindeutig ("11" gibt es
	// unter Windows und unter Debian). Der Cacheschluessel muss die Familie
	// deshalb enthalten.
	key := "version:" + osName + "/" + osVersion
	if s.refs.has(key) {
		return nil
	}
	// Fremdschluessel muessen bei core/create als SUCHOBJEKT uebergeben werden,
	// nicht als Zeichenkette. iTop liest einen String als OQL-Ausdruck und
	// scheitert an allem, was nicht wie OQL aussieht:
	//
	//   Error: osfamily_id: Unexpected token NUMVAL - found '11' at 8 in 'Windows 11 Pro'
	//
	// Mit einem Objekt sucht iTop nach den angegebenen Feldern.
	if err := s.itop.ensureByName("OSVersion", osVersion, map[string]any{
		"osfamily_id": map[string]any{"name": osName},
	}); err != nil {
		return err
	}
	s.refs.add(key)
	return nil
}
