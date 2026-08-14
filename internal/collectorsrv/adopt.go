package collectorsrv

import (
	"fmt"
	"strings"
)

// Uebernahme bestehender VirtualMachine-CIs.
//
// Das Problem, das hier geloest wird:
//
// VMs entstehen in iTop aus einer Hypervisor-Quelle (vCenter, Proxmox), weil nur
// die den Pflicht-Fremdschluessel virtualhost_id kennt. Diese Quelle setzt aber
// kein agent_guid - das Feld gehoert dem itop-agent. Gleichzeitig hat die Klasse
// VirtualMachine in iTop KEIN Attribut serialnumber, die Reimaging-Aufloesung
// greift also auch nicht.
//
// Folge ohne diesen Schritt: der Collector findet die VM nie, weist jede Meldung
// mit 409 ab, und zwar dauerhaft. Die VM waere fuer den Agent unerreichbar.
//
// Loesung: einmalige Uebernahme ueber den Namen. Findet sich genau eine
// VirtualMachine mit diesem Namen, traegt der Collector die agent_guid per REST
// nach. Ab dann laeuft alles Weitere ueber die Synchro Data Source wie bei jeder
// anderen Klasse.
//
// Warum hier REST und nicht der Import: die Datasource reconciled ueber
// agent_guid - genau den Wert, der noch fehlt. Das ist ein Henne-Ei-Problem, das
// sich nur ausserhalb des Imports aufloesen laesst. Es passiert einmal pro VM,
// nicht bei jeder Meldung.

// AdoptVirtualMachine verbindet eine gemeldete VM mit einem bestehenden CI.
//
// Rueckgabe: true, wenn eine VM uebernommen wurde.
func (s *Service) AdoptVirtualMachine(guid, hostname string) (bool, error) {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return false, nil
	}
	candidates, err := s.itop.query("VirtualMachine",
		fmt.Sprintf("name = '%s'", oqlEscape(hostname)), "id,name,agent_guid")
	if err != nil {
		return false, fmt.Errorf("VirtualMachine %q suchen: %w", hostname, err)
	}

	// Bereits von einem anderen Agent belegte CIs kommen nicht in Frage - sonst
	// wuerden zwei Maschinen gleichen Namens einander ueberschreiben.
	free := candidates[:0]
	for _, ci := range candidates {
		if ci.AgentGUID == "" {
			free = append(free, ci)
		}
	}

	switch len(free) {
	case 0:
		return false, nil
	case 1:
		ci := free[0]
		if err := s.itop.setAgentGUID("VirtualMachine", ci.ID, guid); err != nil {
			return false, err
		}
		s.log.Info("bestehende VirtualMachine uebernommen",
			"ci_id", ci.ID, "name", hostname, "agent_guid", guid)
		return true, nil
	default:
		ids := make([]int, 0, len(free))
		for _, ci := range free {
			ids = append(ids, ci.ID)
		}
		// Nicht raten. Zwei VMs gleichen Namens auf verschiedenen Hypervisoren
		// sind in iTop zulaessig - welche gemeint ist, weiss nur ein Mensch.
		return false, fmt.Errorf("mehrere VirtualMachine-CIs mit Namen %q: ids=%v", hostname, ids)
	}
}

// setAgentGUID traegt die agent_guid an einem bestehenden CI nach.
func (c *ITopClient) setAgentGUID(class string, id int, guid string) error {
	res, err := c.rest(map[string]any{
		"operation":     "core/update",
		"class":         class,
		"key":           id,
		"fields":        map[string]any{"agent_guid": guid},
		"output_fields": "id,agent_guid",
		"comment":       "itop-agent collector: bestehendes CI uebernommen",
	})
	if err != nil {
		return fmt.Errorf("agent_guid an %s %d setzen: %w", class, id, err)
	}
	for _, o := range res.Objects {
		if o.Code != 0 {
			return fmt.Errorf("agent_guid an %s %d setzen: %s", class, id, o.Message)
		}
	}
	return nil
}
