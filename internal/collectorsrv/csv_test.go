package collectorsrv

import (
	"encoding/csv"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JxxKal/itop-collector/internal/report"
)

func sampleReport() *report.Report {
	return &report.Report{
		AgentGUID:    "11111111-1111-4111-8111-111111111111",
		AgentVersion: "0.1.0",
		CollectedAt:  time.Date(2026, 8, 13, 14, 0, 0, 0, time.UTC),
		OSFamily:     report.OSWindows,
		Hostname:     "wks-alpha",
		SerialNumber: "SN-ALPHA-001",
		OSName:       "Windows 11 Pro",
		OSVersion:    "10.0.22631",
		CPU:          "Intel i5-1145G7",
		RAMBytes:     16 * 1024 * 1024 * 1024,
		Interfaces: []report.Interface{
			{Description: "lo", MAC: "", IPs: []string{"127.0.0.1"}},
			{Description: "eth0", MAC: "AA:BB:CC:00:00:01", IPs: []string{"192.0.2.10"}},
		},
	}
}

func TestBuildCSVPCEnthaeltMacAdresse(t *testing.T) {
	rep := sampleReport()
	csv, err := BuildCSV(ClassPC, []CSVRow{{Report: rep, GUID: rep.AgentGUID, OrgID: "3"}})
	if err != nil {
		t.Fatalf("BuildCSV: %v", err)
	}
	header, row, _ := strings.Cut(csv, "\n")
	if !strings.Contains(header, "macaddress") {
		t.Errorf("PC-Spalten muessen macaddress enthalten, Header war: %s", header)
	}
	// Die erste Schnittstelle hat keine MAC - es muss die von eth0 genommen werden.
	if !strings.Contains(row, "AA:BB:CC:00:00:01") {
		t.Errorf("erste nicht-leere MAC erwartet, Zeile war: %s", row)
	}
	// RAM wird in MiB gefuehrt, nicht in Bytes.
	if !strings.Contains(row, "16384") {
		t.Errorf("RAM in MiB erwartet (16384), Zeile war: %s", row)
	}
}

// Server und VirtualMachine haben in iTop kein Attribut macaddress (in M0 an
// der Instanz nachgemessen). Wuerde der Collector die Spalte trotzdem senden,
// scheiterte der Import.
func TestBuildCSVOhneMacAdresseFuerServerUndVM(t *testing.T) {
	for _, cls := range []TargetClass{ClassServer, ClassVirtualMachine} {
		rep := sampleReport()
		csv, err := BuildCSV(cls, []CSVRow{{Report: rep, GUID: rep.AgentGUID, OrgID: "3"}})
		if err != nil {
			t.Fatalf("%s: BuildCSV: %v", cls, err)
		}
		header, _, _ := strings.Cut(csv, "\n")
		if strings.Contains(header, "macaddress") {
			t.Errorf("%s kennt kein macaddress, Header war: %s", cls, header)
		}
	}
	// VirtualMachine kennt zusaetzlich keine Seriennummer.
	rep := sampleReport()
	csv, _ := BuildCSV(ClassVirtualMachine, []CSVRow{{Report: rep, GUID: rep.AgentGUID, OrgID: "3"}})
	header, _, _ := strings.Cut(csv, "\n")
	if strings.Contains(header, "serialnumber") {
		t.Errorf("VirtualMachine kennt kein serialnumber, Header war: %s", header)
	}
}

// primary_key ist die quellseitige Kennung des Replicas. Bleibt er leer, landen
// ALLE Zeilen auf demselben Replica und ueberschreiben sich gegenseitig - ohne
// Fehlermeldung. Genau das ist in M0 passiert, aus drei Maschinen wurde eine.
func TestBuildCSVPrimaryKeyIstGefuellt(t *testing.T) {
	rep := sampleReport()
	csv, err := BuildCSV(ClassPC, []CSVRow{{Report: rep, GUID: "abc-123", OrgID: "3"}})
	if err != nil {
		t.Fatalf("BuildCSV: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	cols := strings.Split(lines[0], ";")
	vals := strings.Split(lines[1], ";")
	if cols[0] != "primary_key" {
		t.Fatalf("primary_key muss erste Spalte sein, war %q", cols[0])
	}
	if vals[0] != "abc-123" {
		t.Errorf("primary_key muss die verwendete GUID sein, war %q", vals[0])
	}
	if vals[1] != "abc-123" {
		t.Errorf("agent_guid muss die verwendete GUID sein, war %q", vals[1])
	}
}

// Ein Semikolon im Wert wuerde die Spalten verschieben.
func TestCSVEscapeSchuetztVorTrennerImWert(t *testing.T) {
	rep := sampleReport()
	rep.CPU = "Xeon Gold; 32 Kerne"
	rep.Hostname = `wks-"quote"`
	out, err := BuildCSV(ClassPC, []CSVRow{{Report: rep, GUID: rep.AgentGUID, OrgID: "3"}})
	if err != nil {
		t.Fatalf("BuildCSV: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if want := `"Xeon Gold; 32 Kerne"`; !strings.Contains(lines[1], want) {
		t.Errorf("Trenner im Wert muss maskiert werden, erwartet %s in: %s", want, lines[1])
	}
	if want := `"wks-""quote"""`; !strings.Contains(lines[1], want) {
		t.Errorf("Anfuehrungszeichen muessen verdoppelt werden, erwartet %s in: %s", want, lines[1])
	}
	// Spaltenzahl muss trotz Sonderzeichen stimmen. Mit einem echten Parser
	// pruefen und nicht Semikolons zaehlen - der Trenner im maskierten Wert
	// zaehlt sonst mit, und der Test schlaegt fehl, obwohl die CSV stimmt.
	r := csv.NewReader(strings.NewReader(out))
	r.Comma = ';'
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("erzeugte CSV ist nicht parsebar: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("Kopf + eine Zeile erwartet, bekam %d", len(records))
	}
	if len(records[0]) != len(records[1]) {
		t.Errorf("Spaltenzahl verschoben: Kopf %d, Zeile %d", len(records[0]), len(records[1]))
	}
	// Der Wert muss beim Parsen wieder unmaskiert herauskommen.
	idx := indexOf(records[0], "cpu")
	if idx < 0 || records[1][idx] != "Xeon Gold; 32 Kerne" {
		t.Errorf("cpu nach dem Parsen falsch: %q", records[1][idx])
	}
}

func indexOf(xs []string, want string) int {
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}

// Ohne Hostname waere das CI in iTop nicht auffindbar.
func TestBuildCSVFaelltAufGUIDZurueckWennHostnameFehlt(t *testing.T) {
	rep := sampleReport()
	rep.Hostname = ""
	csv, _ := BuildCSV(ClassPC, []CSVRow{{Report: rep, GUID: "guid-42", OrgID: "3"}})
	if !strings.Contains(csv, "guid-42;guid-42;guid-42") {
		t.Errorf("name muss auf die GUID zurueckfallen, CSV war:\n%s", csv)
	}
}

func TestOQLEscapeMaskiertApostroph(t *testing.T) {
	if got := oqlEscape("O'Brien"); got != `O\'Brien` {
		t.Errorf("oqlEscape: got %q", got)
	}
}

// Der Import liefert seine Zusammenfassung ueber HTTP mit fuehrenden
// Leerzeichen und HTML-Umbruch. Ein zu strenges Muster zaehlt dann stumm
// Nullen, obwohl der Import erfolgreich war.
func TestSummaryParserVerstehtHTTPFormat(t *testing.T) {
	body := `<p>#------------------------------------------------------------<br/>
  # Import phase summary<br/>
  #Objects created: 3 (0 warnings)<br/>
  #Objects creation errors: 0<br/>
  #Objects updated: 2 (0 warnings)<br/>
  #Objects reconciled (unchanged): 1 (0 warnings)<br/>
  #Objects reconciliation errors: 4<br/></p>`

	var got ImportResult
	for _, m := range summaryLine.FindAllStringSubmatch(body, -1) {
		switch m[1] {
		case "Objects created":
			got.Created = atoiTest(t, m[2])
		case "Objects updated":
			got.Updated = atoiTest(t, m[2])
		case "Objects reconciled (unchanged)":
			got.Unchanged = atoiTest(t, m[2])
		case "Objects creation errors":
			got.CreationErrors = atoiTest(t, m[2])
		case "Objects reconciliation errors":
			got.ReconcileErrors = atoiTest(t, m[2])
		}
	}
	want := ImportResult{Created: 3, Updated: 2, Unchanged: 1, CreationErrors: 0, ReconcileErrors: 4}
	if got != want {
		t.Errorf("Zusammenfassung falsch geparst:\n got %+v\nwant %+v", got, want)
	}
	if !got.HasErrors() {
		t.Error("HasErrors muss bei reconciliation errors > 0 wahr sein")
	}
}

func atoiTest(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("Atoi(%q): %v", s, err)
	}
	return n
}
