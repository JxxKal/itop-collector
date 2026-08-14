package collectorsrv

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/JxxKal/itop-collector/internal/report"
)

// Die Spaltensaetze sind PRO ZIELKLASSE verschieden, weil iTops Klassen sich
// unterscheiden. In M0 an der Instanz nachgemessen:
//
//	Server         hat kein macaddress, type, ipaddress_id
//	VirtualMachine hat zusaetzlich kein serialnumber, brand_id, model_id,
//	               location_id, asset_number, purchase_date, end_of_warranty
//
// macaddress gibt es also NUR an PC. Ein einheitliches Mapping fuer alle drei
// Klassen gibt es damit nicht - wer eine Spalte schickt, die die Zielklasse
// nicht kennt, bekommt vom Import einen Fehler.
//
// org_id steht bewusst mit drin, obwohl der Agent organisatorische Zuordnungen
// nie anfassen soll: an PC/Server ist es Pflichtfeld und muss bei der ANLAGE
// gesetzt werden. Die Datasource fuehrt es mit update_policy=write_if_empty,
// damit es danach unangetastet bleibt.
var columnsByClass = map[TargetClass][]string{
	ClassPC: {
		"primary_key", "agent_guid", "name", "org_id", "serialnumber",
		"cpu", "ram", "macaddress", "osfamily_id", "osversion_id", "agent_last_seen",
	},
	ClassServer: {
		"primary_key", "agent_guid", "name", "org_id", "serialnumber",
		"cpu", "ram", "osfamily_id", "osversion_id", "agent_last_seen",
	},
	ClassVirtualMachine: {
		"primary_key", "agent_guid", "name", "org_id",
		"cpu", "ram", "osfamily_id", "osversion_id", "agent_last_seen",
	},
}

// itopTimeLayout ist das Format, das der Import mit date_format=Y-m-d H:i:s erwartet.
const itopTimeLayout = "2006-01-02 15:04:05"

// buildRow erzeugt die Werte einer CSV-Zeile in der Reihenfolge der Spalten.
//
// guid ist die GUID, unter der gemeldet wird - nach einer Reimaging-Aufloesung
// kann das eine andere sein als die im Report. primary_key ist bewusst dieselbe
// GUID: er ist die quellseitige Kennung des Replicas und MUSS eindeutig gefuellt
// sein. Bleibt er leer, landen alle Zeilen auf demselben Replica und
// ueberschreiben sich gegenseitig - ohne Fehlermeldung (in M0 passiert).
func buildRow(cls TargetClass, rep *report.Report, guid, orgID string) map[string]string {
	name := rep.Hostname
	if name == "" {
		name = guid // ohne Namen waere das CI in iTop nicht auffindbar
	}
	return map[string]string{
		"primary_key":     guid,
		"agent_guid":      guid,
		"name":            name,
		"org_id":          orgID,
		"serialnumber":    rep.SerialNumber,
		"cpu":             rep.CPU,
		"ram":             strconv.FormatInt(rep.RAMMebibytes(), 10),
		"macaddress":      rep.PrimaryMAC(),
		"osfamily_id":     rep.OSName,
		"osversion_id":    rep.OSVersion,
		"agent_last_seen": rep.CollectedAt.UTC().Format(itopTimeLayout),
	}
}

// csvEscape setzt einen Wert nach RFC4180 in Anfuehrungszeichen, wenn noetig.
//
// Der Trenner ist ';'. Ein Modellname wie "PowerEdge R750; Rack" wuerde die
// Zeile sonst verschieben und Werte in die falschen Spalten kippen.
func csvEscape(s string) string {
	if !strings.ContainsAny(s, ";\"\n\r") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// BuildCSV baut die Importdatei fuer eine Zielklasse.
//
// Alle Reports muessen zur selben Zielklasse gehoeren - jede Klasse hat ihre
// eigene Datasource mit eigenem Spaltensatz.
func BuildCSV(cls TargetClass, rows []CSVRow) (string, error) {
	cols, ok := columnsByClass[cls]
	if !ok {
		return "", fmt.Errorf("keine Spaltendefinition fuer Zielklasse %q", cls)
	}
	var b strings.Builder
	b.WriteString(strings.Join(cols, ";"))
	b.WriteString("\n")

	for _, row := range rows {
		values := buildRow(cls, row.Report, row.GUID, row.OrgID)
		fields := make([]string, 0, len(cols))
		for _, c := range cols {
			fields = append(fields, csvEscape(values[c]))
		}
		b.WriteString(strings.Join(fields, ";"))
		b.WriteString("\n")
	}
	return b.String(), nil
}

// CSVRow verbindet eine Meldung mit den Werten, die erst der Collector kennt.
type CSVRow struct {
	Report *report.Report
	GUID   string // ggf. die uebernommene GUID nach Reimaging-Aufloesung
	OrgID  string
}
