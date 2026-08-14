package collectorsrv

import (
	"testing"

	"github.com/JxxKal/itop-collector/internal/report"
)

// mustGroup baut eine Gruppe wie beim Lesen aus iTop.
func mustGroup(t *testing.T, id int, name, patterns string) *SoftwareGroup {
	t.Helper()
	inc, exc, err := parsePatterns(patterns)
	if err != nil {
		t.Fatalf("parsePatterns(%q): %v", patterns, err)
	}
	return &SoftwareGroup{ID: id, Name: name, includes: inc, excludes: exc}
}

// Die Namen stammen aus echten Windows-Installationen (Registry-Uninstall).
// Genau an diesen Schreibweisen muss sich die Zuordnung bewaehren.
func TestMatchesDotNet(t *testing.T) {
	g := mustGroup(t, 1, ".Net Framework", ".net\n!.NET Reflector")

	treffer := []string{
		"Microsoft .NET Framework 4.8.1",
		"Microsoft .NET Framework 4.7.2 Targeting Pack",
		"ASP.NET Core Runtime 8.0.11 (x64)",
		"Microsoft .NET Runtime - 8.0.11 (x64)",
		"Microsoft .NET Host - 6.0.36 (x64)",
		"microsoft asp.net core module v2",
	}
	for _, n := range treffer {
		if !g.Matches(n) {
			t.Errorf("sollte treffen: %q", n)
		}
	}

	danebem := []string{
		"Telnet Client",      // enthaelt "net", aber nicht ".net"
		"Netzwerk-Assistent", // dito
		"Red Gate .NET Reflector",
	}
	for _, n := range danebem {
		if g.Matches(n) {
			t.Errorf("sollte NICHT treffen: %q", n)
		}
	}
}

// Der Fall, der die ganze Anforderung begruendet: zwanzig .NET-Versionen
// duerfen die Gruppe nur EINMAL erzeugen.
func TestMatchGroupsEntdoppelt(t *testing.T) {
	groups := []*SoftwareGroup{
		mustGroup(t, 1, ".Net Framework", ".net"),
		mustGroup(t, 2, "Browser", "chrome\nfirefox\nmicrosoft edge"),
	}
	sw := []report.Software{
		{Name: "Microsoft .NET Framework 4.8.1"},
		{Name: "Microsoft .NET Framework 4.7.2"},
		{Name: "Microsoft .NET Framework 3.5"},
		{Name: "ASP.NET Core Runtime 8.0.11"},
		{Name: "Google Chrome"},
		{Name: "Mozilla Firefox (x64 de)"},
		{Name: "Irgendein Fachprogramm 2.1"},
	}
	got := MatchGroups(groups, sw)
	if len(got) != 2 {
		t.Fatalf("zwei Gruppen erwartet, bekam %d", len(got))
	}
	namen := map[string]bool{got[0].Name: true, got[1].Name: true}
	if !namen[".Net Framework"] || !namen["Browser"] {
		t.Errorf("falsche Gruppen: %v", namen)
	}
}

// "Edge" allein waere zu weit - es steckt auch in anderen Produktnamen.
// Dafuer gibt es den regulaeren Ausdruck.
func TestRegexMusterFuerEngeTreffer(t *testing.T) {
	g := mustGroup(t, 2, "Browser", `/^microsoft edge( |$)/`)

	if !g.Matches("Microsoft Edge") {
		t.Error("sollte treffen: Microsoft Edge")
	}
	if !g.Matches("Microsoft Edge WebView2 Runtime") {
		t.Error("sollte treffen: Microsoft Edge WebView2 Runtime")
	}
	if g.Matches("Edge Diagnostics Adapter") {
		t.Error("sollte NICHT treffen: Edge Diagnostics Adapter")
	}
}

// Ausschluesse muessen Einschluesse schlagen, sonst laesst sich keine Ausnahme
// pflegen.
func TestAusschlussSchlaegtEinschluss(t *testing.T) {
	g := mustGroup(t, 3, "Java JRE", "java\n!Java Auto Updater\n!JavaScript")

	if !g.Matches("Java 8 Update 421 (64-bit)") {
		t.Error("sollte treffen: Java 8 Update 421")
	}
	for _, n := range []string{"Java Auto Updater", "JavaScript Runtime"} {
		if g.Matches(n) {
			t.Errorf("sollte NICHT treffen: %q", n)
		}
	}
}

func TestParsePatternsSyntax(t *testing.T) {
	inc, exc, err := parsePatterns("  .net  \n\n# Kommentar\n!Reflector\n/^abc$/\n")
	if err != nil {
		t.Fatalf("parsePatterns: %v", err)
	}
	if len(inc) != 2 {
		t.Errorf("zwei Einschluesse erwartet, bekam %d", len(inc))
	}
	if len(exc) != 1 {
		t.Errorf("ein Ausschluss erwartet, bekam %d", len(exc))
	}
	// Kommentare und Leerzeilen duerfen nicht als Muster durchrutschen -
	// ein leeres Muster wuerde sonst auf ALLES passen.
	for _, m := range append(inc, exc...) {
		if m.substr == "" && m.re == nil {
			t.Error("leeres Muster durchgerutscht - wuerde auf alles passen")
		}
	}
}

func TestParsePatternsMeldetKaputteRegex(t *testing.T) {
	if _, _, err := parsePatterns("/([unvollstaendig/"); err == nil {
		t.Error("fehlerhafter regulaerer Ausdruck haette einen Fehler liefern muessen")
	}
}

// Was nirgends passt, ist die Grundlage zum Erweitern der Liste.
func TestUnmatched(t *testing.T) {
	groups := []*SoftwareGroup{mustGroup(t, 1, ".Net Framework", ".net")}
	sw := []report.Software{
		{Name: "Microsoft .NET Framework 4.8.1"},
		{Name: "TeBIS3 Windows Client (64 bit)"},
		{Name: ""},
	}
	got := Unmatched(groups, sw)
	if len(got) != 1 || got[0] != "TeBIS3 Windows Client (64 bit)" {
		t.Errorf("erwartet [TeBIS3 …], bekam %v", got)
	}
}

// Eine Gruppe ohne Muster darf nicht auf alles passen.
func TestGruppeOhneMusterTrifftNichts(t *testing.T) {
	g := mustGroup(t, 9, "Leer", "")
	if g.Matches("Irgendwas") {
		t.Error("Gruppe ohne Muster darf nichts treffen")
	}
}
