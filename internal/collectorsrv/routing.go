package collectorsrv

import (
	"errors"
	"fmt"
	"strings"

	"github.com/JxxKal/itop-collector/internal/report"
)

// TargetClass ist die iTop-Zielklasse eines Geraets.
type TargetClass string

const (
	ClassPC             TargetClass = "PC"
	ClassServer         TargetClass = "Server"
	ClassVirtualMachine TargetClass = "VirtualMachine"
)

// ErrAmbiguousSerial meldet, dass mehrere CIs dieselbe Seriennummer tragen.
// In dem Fall wird bewusst NICHT geraten - der Report bleibt liegen.
var ErrAmbiguousSerial = errors.New("mehrere CIs mit derselben Seriennummer")

// ErrVMNeedsHypervisor meldet, dass eine unbekannte VM nicht angelegt werden kann.
//
// VirtualMachine.virtualhost_id ist in iTop ein Pflicht-Fremdschluessel auf
// VirtualHost. Eine VM weiss von innen nicht, auf welchem Hypervisor sie laeuft -
// der Agent kann den Wert also nie liefern. In M0 nachgewiesen:
//
//	Unable to create destination object:
//	  Ungueltiger Attributwert 'Host' (virtualhost_id) : Null not allowed
//
// VMs muessen deshalb aus einer Hypervisor-Quelle stammen; der Agent ergaenzt
// sie nur.
var ErrVMNeedsHypervisor = errors.New("VirtualMachine existiert nicht in iTop und kann vom Agent nicht angelegt werden")

// classifyFresh leitet die Zielklasse aus einer Meldung ab.
//
// NUR fuer Geraete ohne Eintrag in der Registry. Einmal entschieden, bleibt die
// Klasse an der GUID kleben - siehe ResolveClass.
func classifyFresh(rep *report.Report) TargetClass {
	name := strings.ToLower(rep.OSName)

	// Virtualisierung schlaegt alles andere: eine VM ist in iTop eine
	// VirtualMachine, unabhaengig davon, welches Betriebssystem darauf laeuft.
	//
	// "container" zaehlt NICHT dazu. Ein Container ist kein CI im Sinne der
	// CMDB - er hat keine eigene Hardware und verschwindet beim naechsten
	// Deployment. Solche Meldungen laufen weiter als PC/Server, was zumindest
	// sichtbar ist; ausfiltern gehoert in eine spaetere Ausbaustufe.
	if rep.Virtualization != "" && rep.Virtualization != "container" {
		return ClassVirtualMachine
	}

	// Windows Server nennt sich in Win32_OperatingSystem.Caption selbst so.
	if rep.OSFamily == report.OSWindows && strings.Contains(name, "server") {
		return ClassServer
	}
	// Unter Linux ist die Unterscheidung Client/Server nicht aus dem OS-Namen
	// ablesbar. Desktop-Distributionen sind die Ausnahme, alles andere wird als
	// Server behandelt - das ist die konservativere Annahme.
	if rep.OSFamily == report.OSLinux {
		for _, desktop := range []string{"ubuntu desktop", "fedora workstation", "linux mint", "pop!_os"} {
			if strings.Contains(name, desktop) {
				return ClassPC
			}
		}
		return ClassServer
	}
	return ClassPC
}

// ResolveClass bestimmt die Zielklasse eines Geraets - stabil.
//
// Warum stabil und nicht bei jedem Report neu: jede Synchro Data Source
// reconciled nur innerhalb ihrer scope_class. Wandert ein Geraet von PC nach
// Server, findet die Server-Datasource das vorhandene PC-CI NICHT und legt ein
// zweites an - fehlerfrei und unbemerkt. In M0 reproduziert.
//
// Die einmal getroffene Entscheidung steht deshalb in der Registry und wird nur
// dort geaendert, bewusst und nachvollziehbar.
func (s *Service) ResolveClass(dev *Device, rep *report.Report) TargetClass {
	if dev.TargetClass != "" {
		guessed := classifyFresh(rep)
		if guessed != dev.TargetClass {
			// Kein Wechsel, aber sichtbar machen: entweder die Heuristik ist
			// schlechter geworden oder das Geraet hat sich wirklich geaendert.
			// Beides will ein Mensch wissen.
			s.log.Warn("Klassen-Heuristik weicht von der festgelegten Klasse ab",
				"agent_guid", rep.AgentGUID,
				"festgelegt", dev.TargetClass,
				"heuristik", guessed,
				"hinweis", "Klasse bleibt unveraendert; Wechsel wuerde ein zweites CI erzeugen")
		}
		return dev.TargetClass
	}
	cls := classifyFresh(rep)
	dev.TargetClass = cls
	s.log.Info("Zielklasse festgelegt", "agent_guid", rep.AgentGUID, "klasse", cls)
	return cls
}

// ResolveReimaging behandelt den Fall "neu aufgesetzt".
//
// Nach einem Reimaging erzeugt der Agent eine neue GUID, die Seriennummer bleibt.
// iTop kann das nicht aufloesen: mehrere Reconciliation-Attribute werden mit AND
// verknuepft, einen Fallback auf einen zweiten Schluessel gibt es nicht
// (synchrodatasource.class.inc.php:2203). Ohne diesen Schritt entsteht bei jedem
// Reimaging eine Dublette - in M0 reproduziert, fehlerfrei und ohne Warnung.
//
// Rueckgabe ist die GUID, unter der gemeldet werden soll: entweder die
// urspruengliche des vorhandenen CIs oder die neue.
func (s *Service) ResolveReimaging(rep *report.Report) (string, error) {
	// Kennt iTop die GUID schon, ist nichts zu tun.
	existing, err := s.itop.FindByAgentGUID(rep.AgentGUID)
	if err != nil {
		return "", fmt.Errorf("Suche nach agent_guid: %w", err)
	}
	if len(existing) > 0 {
		return rep.AgentGUID, nil
	}

	// Unbekannte GUID: koennte ein neues Geraet sein oder ein neu aufgesetztes.
	// Die Seriennummer entscheidet. Ist sie leer (OEM-Platzhalter), bleibt es
	// bei "neues Geraet" - raten waere hier schlimmer als eine Dublette.
	if strings.TrimSpace(rep.SerialNumber) == "" {
		return rep.AgentGUID, nil
	}

	bySerial, err := s.itop.FindBySerial(rep.SerialNumber)
	if err != nil {
		return "", fmt.Errorf("Suche nach serialnumber: %w", err)
	}
	switch len(bySerial) {
	case 0:
		return rep.AgentGUID, nil
	case 1:
		old := bySerial[0]
		if old.AgentGUID == "" {
			// CI existiert, wurde aber nie vom Agent gemeldet (z.B. von Hand
			// angelegt). Die neue GUID darf es uebernehmen.
			s.log.Info("vorhandenes CI ohne GUID uebernommen",
				"ci_id", old.ID, "serial", rep.SerialNumber, "agent_guid", rep.AgentGUID)
			return rep.AgentGUID, nil
		}
		s.log.Info("Reimaging erkannt - vorhandene GUID wird weiterbenutzt",
			"ci_id", old.ID, "serial", rep.SerialNumber,
			"gemeldete_guid", rep.AgentGUID, "bestehende_guid", old.AgentGUID)
		return old.AgentGUID, nil
	default:
		ids := make([]int, 0, len(bySerial))
		for _, ci := range bySerial {
			ids = append(ids, ci.ID)
		}
		return "", fmt.Errorf("%w: serial=%q ci_ids=%v", ErrAmbiguousSerial, rep.SerialNumber, ids)
	}
}
