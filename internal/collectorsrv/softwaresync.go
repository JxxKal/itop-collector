package collectorsrv

import (
	"fmt"
	"strconv"

	"github.com/JxxKal/itop-collector/internal/report"
)

// Pflege der Verknuepfungen CI <-> Softwaregruppe.
//
// In iTop heisst diese Verknuepfung SoftwareInstance: sie verbindet ein
// FunctionalCI (system_id) mit einem Katalogeintrag (software_id). Fuer jede
// Gruppe, deren Muster auf irgendein installiertes Programm passen, entsteht
// genau EINE solche Verknuepfung - unabhaengig davon, wie viele Versionen
// tatsaechlich installiert sind.
//
// Bewusst ueber REST und nicht ueber eine Synchro Data Source: es geht um
// abgeleitete Verknuepfungen, nicht um Attribute eines CIs. Eine eigene Data
// Source dafuer muesste ueber das Paar (System, Software) abgleichen, und die
// Spaltenlogik einer festen CSV passt schlecht zu einer Menge, die je Maschine
// unterschiedlich gross ist.

// SyncSoftwareGroups bringt die Verknuepfungen eines CIs auf den gemeldeten Stand.
func (s *Service) SyncSoftwareGroups(guid string, software []report.Software) error {
	if len(software) == 0 {
		// Kein Softwareinventar gemeldet heisst NICHT "keine Software
		// installiert" - aeltere Agenten senden das Feld nicht, und bei einem
		// haengenden Paketmanager bleibt es leer. Nichts anfassen.
		return nil
	}

	groups, err := s.SoftwareGroups()
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		// Kein Katalogeintrag mit Mustern - die Zuordnung ist schlicht nicht
		// eingerichtet. Das ist kein Fehler.
		return nil
	}

	cis, err := s.itop.FindByAgentGUID(guid)
	if err != nil || len(cis) != 1 {
		return err
	}
	ci := cis[0]

	matched := MatchGroups(groups, software)
	s.rememberUnmatched(Unmatched(groups, software))

	soll := make(map[int]*SoftwareGroup, len(matched))
	for _, g := range matched {
		soll[g.ID] = g
	}

	// Nur Verknuepfungen zu Gruppen anfassen, die WIR verwalten. Eine
	// SoftwareInstance, die jemand von Hand angelegt hat oder die zu einem
	// Katalogeintrag ohne Muster gehoert, bleibt unberuehrt.
	verwaltet := make(map[int]bool, len(groups))
	for _, g := range groups {
		verwaltet[g.ID] = true
	}

	ist, err := s.itop.softwareInstances(ci.ID)
	if err != nil {
		return err
	}

	var angelegt, entfernt int
	for softwareID, instanceID := range ist {
		if !verwaltet[softwareID] {
			continue // fremde Verknuepfung
		}
		if _, ok := soll[softwareID]; !ok {
			// Die Software ist nicht mehr installiert.
			if err := s.itop.deleteSoftwareInstance(instanceID); err != nil {
				return err
			}
			entfernt++
		}
	}
	for softwareID, g := range soll {
		if _, ok := ist[softwareID]; ok {
			continue
		}
		if err := s.itop.createSoftwareInstance(ci, softwareID, g.Name); err != nil {
			return err
		}
		angelegt++
	}

	if angelegt > 0 || entfernt > 0 {
		s.log.Info("Softwaregruppen aktualisiert",
			"ci_id", ci.ID, "angelegt", angelegt, "entfernt", entfernt,
			"gesamt", len(soll))
	}
	return nil
}

// rememberUnmatched zaehlt Programmnamen ohne Gruppe.
func (s *Service) rememberUnmatched(names []string) {
	if len(names) == 0 {
		return
	}
	s.unmatchedMu.Lock()
	defer s.unmatchedMu.Unlock()
	for _, n := range names {
		s.unmatched[n]++
	}
	// Obergrenze, damit eine Flotte mit vielen Fachprogrammen den Speicher
	// nicht unbegrenzt fuellt. Beim Ueberlauf wird geleert statt gedeckelt -
	// die Liste ist ein Arbeitsmittel, kein Bestand.
	if len(s.unmatched) > 5000 {
		s.unmatched = map[string]int{}
	}
}

// softwareInstances liefert die vorhandenen Verknuepfungen eines CIs
// als Abbildung software_id -> instance_id.
func (c *ITopClient) softwareInstances(ciID int) (map[int]int, error) {
	res, err := c.rest(map[string]any{
		"operation":     "core/get",
		"class":         "SoftwareInstance",
		"key":           fmt.Sprintf("SELECT SoftwareInstance WHERE system_id = %d", ciID),
		"output_fields": "id,software_id",
	})
	if err != nil {
		return nil, fmt.Errorf("Softwareverknuepfungen von CI %d lesen: %w", ciID, err)
	}
	out := make(map[int]int, len(res.Objects))
	for _, o := range res.Objects {
		instanceID, _ := strconv.Atoi(o.Fields["id"])
		softwareID, _ := strconv.Atoi(o.Fields["software_id"])
		if softwareID != 0 {
			out[softwareID] = instanceID
		}
	}
	return out, nil
}

func (c *ITopClient) createSoftwareInstance(ci CI, softwareID int, groupName string) error {
	res, err := c.rest(map[string]any{
		"operation": "core/create",
		// PCSoftware statt der abstrakten Oberklasse: SoftwareInstance selbst
		// laesst sich nicht anlegen, und PCSoftware ist die Auspraegung fuer
		// Arbeitsplatz- und Serversoftware.
		"class": "PCSoftware",
		"fields": map[string]any{
			"system_id":   ci.ID,
			"software_id": softwareID,
			"status":      "active",
			// SoftwareInstance erbt von FunctionalCI - name und org_id sind
			// dort Pflicht. Der Name wird sprechend gewaehlt, damit die
			// Verknuepfung in Listen ohne Aufklappen lesbar ist.
			"name": fmt.Sprintf("%s @ %s", groupName, ci.Name),
			// Organisation vom CI uebernehmen, nicht aus der Vorgabe: wird ein
			// Geraet in eine andere Organisation verschoben, soll die
			// Verknuepfung mitwandern.
			"org_id": ci.OrgID,
		},
		"output_fields": "id,name",
		"comment":       fmt.Sprintf("itop-agent collector: %s auf %s erkannt", groupName, ci.Name),
	})
	if err != nil {
		return fmt.Errorf("Verknuepfung %s an CI %d anlegen: %w", groupName, ci.ID, err)
	}
	for _, o := range res.Objects {
		if o.Code != 0 {
			return fmt.Errorf("Verknuepfung %s an CI %d anlegen: %s", groupName, ci.ID, o.Message)
		}
	}
	return nil
}

func (c *ITopClient) deleteSoftwareInstance(instanceID int) error {
	res, err := c.rest(map[string]any{
		"operation": "core/delete",
		"class":     "SoftwareInstance",
		"key":       instanceID,
		"simulate":  false,
		"comment":   "itop-agent collector: Software nicht mehr installiert",
	})
	if err != nil {
		return fmt.Errorf("Verknuepfung %d entfernen: %w", instanceID, err)
	}
	for _, o := range res.Objects {
		if o.Code != 0 {
			return fmt.Errorf("Verknuepfung %d entfernen: %s", instanceID, o.Message)
		}
	}
	return nil
}
