package collectorsrv

import (
	"fmt"
	"strconv"
)

// Pflege der primaeren IP-Adresse am CI.
//
// Bewusst NICHT ueber die Synchro Data Source, sondern per REST. Der Grund ist
// die Bedeutung eines leeren Wertes: die Spaltenliste einer Data Source ist
// fest, jede Meldung liefert also jede Spalte. Haette eine Maschine keine
// statische Adresse - weil sie an DHCP haengt - stuende in der Spalte ein
// Leerwert, und der Import wuerde eine im IPAM gepflegte Adresse damit
// UEBERSCHREIBEN.
//
// Schweigen ist aber keine Aussage. Deshalb wird das Feld nur angefasst, wenn
// der Agent tatsaechlich eine dauerhafte Adresse gemeldet hat.
//
// Zur Zielklasse: ipaddress_id gibt es AUSSCHLIESSLICH an PC. An der Instanz
// nachgemessen - Server, VirtualMachine, ConnectableCI und PhysicalDevice
// kennen das Attribut nicht:
//
//	PC              hat ipaddress_id
//	Server          invalid attribute code 'ipaddress_id'
//	VirtualMachine  invalid attribute code 'ipaddress_id'
//
// Fuer Server und VMs ist das nicht der vorgesehene Weg: dort fuehrt TeeMIP die
// Adressen ueber IPInterface-Objekte und lnkIPInterfaceToIPAddress. Das ist
// eine eigene Ausbaustufe.

// classHasIPAddress sagt, ob die Zielklasse ein Feld ipaddress_id besitzt.
func classHasIPAddress(cls TargetClass) bool {
	return cls == ClassPC
}

// SyncPrimaryIP haengt die primaere Adresse ans CI.
//
// ip ist die vom Agent gemeldete, dauerhafte IPv4-Adresse (DHCP hat der Agent
// bereits ausgefiltert). Ein leerer Wert fuehrt zu keiner Aenderung.
func (s *Service) SyncPrimaryIP(cls TargetClass, guid, ip, orgID string) error {
	if ip == "" || !classHasIPAddress(cls) {
		return nil
	}

	cis, err := s.itop.FindByAgentGUID(guid)
	if err != nil || len(cis) != 1 {
		// Ohne eindeutiges CI nichts tun. Ein Fehler hier darf die Meldung nicht
		// scheitern lassen - die Inventardaten sind bereits geschrieben.
		return err
	}
	ci := cis[0]

	ipID, err := s.ensureIPAddress(ip, orgID)
	if err != nil {
		return err
	}

	// Nur schreiben, wenn sich etwas aendert. Sonst erzeugt jede Meldung einen
	// Eintrag in der iTop-Aenderungshistorie, und die waere nach kurzer Zeit
	// unlesbar.
	current, err := s.itop.currentIPAddressID(string(cls), ci.ID)
	if err != nil {
		return err
	}
	if current == ipID {
		return nil
	}

	// Eine Adresse darf nur einem Geraet gehoeren. iTop erzwingt das NICHT -
	// an der Instanz geprueft: dieselbe IPv4Address laesst sich problemlos an
	// zwei CIs haengen, ohne Fehlermeldung.
	//
	// Der haeufigste Grund fuer so eine Meldung ist ein Adresskonflikt im Netz
	// oder ein falsch konfiguriertes Geraet. Beides will man sehen, nicht
	// stillschweigend in die CMDB uebernehmen - deshalb wird die Verknuepfung
	// verweigert und protokolliert.
	if other, err := s.itop.ciWithIPAddress(ipID, ci.ID); err != nil {
		return err
	} else if other != 0 {
		return fmt.Errorf("IP %s haengt bereits an CI %d - nicht uebernommen fuer CI %d",
			ip, other, ci.ID)
	}

	if err := s.itop.setIPAddress(string(cls), ci.ID, ipID); err != nil {
		return err
	}
	s.log.Info("primaere IP am CI gesetzt",
		"ci_id", ci.ID, "klasse", cls, "ip", ip, "ip_id", ipID, "vorher", current)
	return nil
}

// ensureIPAddress sucht die Adresse im IPAM und legt sie bei Bedarf an.
//
// Bewusst suchen statt blind anlegen: TeeMIP verwaltet den Adressraum, und eine
// Adresse darf nur einmal existieren. Ist sie schon da - vom IPAM vergeben oder
// von einem anderen Skript - wird genau die benutzt.
func (s *Service) ensureIPAddress(ip, orgID string) (int, error) {
	found, err := s.itop.rest(map[string]any{
		"operation":     "core/get",
		"class":         "IPv4Address",
		"key":           fmt.Sprintf("SELECT IPv4Address WHERE ip = '%s'", oqlEscape(ip)),
		"output_fields": "id,ip",
	})
	if err != nil {
		return 0, fmt.Errorf("IP %s suchen: %w", ip, err)
	}
	if len(found.Objects) > 1 {
		// Sollte im IPAM nicht vorkommen. Nicht raten - lieber nichts tun und
		// den Zustand sichtbar machen.
		return 0, fmt.Errorf("IP %s existiert mehrfach im IPAM (%d Objekte)", ip, len(found.Objects))
	}
	for _, o := range found.Objects {
		id, err := strconv.Atoi(o.Fields["id"])
		if err != nil {
			return 0, fmt.Errorf("unerwartete id fuer IP %s: %q", ip, o.Fields["id"])
		}
		return id, nil
	}

	org, err := strconv.Atoi(orgID)
	if err != nil {
		return 0, fmt.Errorf("ungueltige Organisation %q: %w", orgID, err)
	}
	created, err := s.itop.rest(map[string]any{
		"operation": "core/create",
		"class":     "IPv4Address",
		// Pflicht sind nur ip und org_id. Status und IP-Konfiguration setzt
		// TeeMIP selbst (beobachtet: status=allocated, ipconfig_id=1).
		"fields":        map[string]any{"ip": ip, "org_id": org},
		"output_fields": "id,ip,status",
		"comment":       "itop-agent collector: vom Agent gemeldete statische Adresse",
	})
	if err != nil {
		return 0, fmt.Errorf("IP %s anlegen: %w", ip, err)
	}
	for _, o := range created.Objects {
		if o.Code != 0 {
			return 0, fmt.Errorf("IP %s anlegen: %s", ip, o.Message)
		}
		id, _ := strconv.Atoi(o.Fields["id"])
		s.log.Info("IP-Adresse im IPAM angelegt", "ip", ip, "ip_id", id, "status", o.Fields["status"])
		return id, nil
	}
	return 0, fmt.Errorf("IP %s anlegen lieferte kein Objekt", ip)
}

// ciWithIPAddress sucht ein anderes CI, das diese Adresse bereits fuehrt.
//
// Liefert 0, wenn die Adresse frei ist oder nur am uebergebenen CI haengt.
func (c *ITopClient) ciWithIPAddress(ipID, exceptCI int) (int, error) {
	res, err := c.rest(map[string]any{
		"operation":     "core/get",
		"class":         "PC",
		"key":           fmt.Sprintf("SELECT PC WHERE ipaddress_id = %d", ipID),
		"output_fields": "id,name",
	})
	if err != nil {
		return 0, fmt.Errorf("Belegung von IP %d pruefen: %w", ipID, err)
	}
	for _, o := range res.Objects {
		id, _ := strconv.Atoi(o.Fields["id"])
		if id != exceptCI {
			return id, nil
		}
	}
	return 0, nil
}

// currentIPAddressID liest die aktuell verknuepfte Adresse.
func (c *ITopClient) currentIPAddressID(class string, ciID int) (int, error) {
	res, err := c.rest(map[string]any{
		"operation":     "core/get",
		"class":         class,
		"key":           ciID,
		"output_fields": "id,ipaddress_id",
	})
	if err != nil {
		return 0, fmt.Errorf("ipaddress_id von %s %d lesen: %w", class, ciID, err)
	}
	for _, o := range res.Objects {
		id, _ := strconv.Atoi(o.Fields["ipaddress_id"])
		return id, nil
	}
	return 0, nil
}

// setIPAddress verknuepft die Adresse mit dem CI.
func (c *ITopClient) setIPAddress(class string, ciID, ipID int) error {
	res, err := c.rest(map[string]any{
		"operation":     "core/update",
		"class":         class,
		"key":           ciID,
		"fields":        map[string]any{"ipaddress_id": ipID},
		"output_fields": "id,ipaddress_id",
		"comment":       "itop-agent collector: primaere IP",
	})
	if err != nil {
		// iTop schraenkt die zulaessigen Werte fuer ipaddress_id ein (abhaengig
		// von der IPConfig und dem Status der Adresse). Eine Ablehnung ist
		// deshalb ein moeglicher Normalfall und kein Grund, die Meldung zu
		// verwerfen - sie muss nur sichtbar sein.
		return fmt.Errorf("IP %d an %s %d haengen: %w", ipID, class, ciID, err)
	}
	for _, o := range res.Objects {
		if o.Code != 0 {
			return fmt.Errorf("IP %d an %s %d haengen: %s", ipID, class, ciID, o.Message)
		}
	}
	return nil
}
