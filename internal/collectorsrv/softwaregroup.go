package collectorsrv

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JxxKal/itop-collector/internal/report"
)

// Zuordnung des rohen Softwareinventars zu einer ueberschaubaren Gruppenliste.
//
// Der Agent meldet, was das Geraet hergibt - "Microsoft .NET Framework 4.8.1",
// "ASP.NET Core Runtime 8.0.11", "Google Chrome", "Mozilla Firefox (x64 de)".
// In der CMDB soll davon nicht jede Version einzeln landen, sondern nur die
// Gruppe: ".Net Framework", "Browser".
//
// Die Regeln stehen NICHT hier im Code, sondern am Katalogeintrag in iTop
// (Software.agent_match_patterns). Eine Gruppe zu ergaenzen heisst damit: einen
// Software-Eintrag in iTop anlegen und Muster hinterlegen - kein Rebuild, kein
// Ausrollen.

// SoftwareGroup ist ein Katalogeintrag mit Zuordnungsmustern.
type SoftwareGroup struct {
	ID       int
	Name     string
	includes []matcher
	excludes []matcher
}

// matcher ist ein einzelnes Muster.
type matcher struct {
	raw    string
	substr string         // fuer den Regelfall: Teilzeichenkette, klein geschrieben
	re     *regexp.Regexp // nur gesetzt, wenn das Muster ein regulaerer Ausdruck ist
}

func (m matcher) matches(nameLower string) bool {
	if m.re != nil {
		return m.re.MatchString(nameLower)
	}
	return strings.Contains(nameLower, m.substr)
}

// parsePatterns liest das Musterfeld eines Katalogeintrags.
//
// Syntax, bewusst knapp gehalten - das pflegen Menschen in einer iTop-Maske:
//
//	.net                     trifft, wenn ".net" im Namen vorkommt (Gross-/Klein egal)
//	!Windows Defender        schliesst aus, auch wenn ein anderes Muster trifft
//	/^microsoft edge( |$)/   regulaerer Ausdruck fuer die Faelle, wo Teiltreffer zu weit greifen
//
// Leere Zeilen und Zeilen mit # am Anfang werden uebersprungen, damit sich die
// Liste kommentieren laesst.
func parsePatterns(text string) (includes, excludes []matcher, err error) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		negate := false
		if strings.HasPrefix(line, "!") {
			negate = true
			line = strings.TrimSpace(line[1:])
			if line == "" {
				continue
			}
		}

		var m matcher
		m.raw = line
		if len(line) >= 2 && strings.HasPrefix(line, "/") && strings.HasSuffix(line, "/") {
			// Regulaerer Ausdruck. Wird gegen den klein geschriebenen Namen
			// geprueft, deshalb ist (?i) meist ueberfluessig - schadet aber nicht.
			re, cerr := regexp.Compile(line[1 : len(line)-1])
			if cerr != nil {
				return nil, nil, fmt.Errorf("ungueltiger regulaerer Ausdruck %q: %w", line, cerr)
			}
			m.re = re
		} else {
			m.substr = strings.ToLower(line)
		}

		if negate {
			excludes = append(excludes, m)
		} else {
			includes = append(includes, m)
		}
	}
	return includes, excludes, nil
}

// Matches sagt, ob ein gemeldeter Programmname zu dieser Gruppe gehoert.
//
// Ausschluesse haben Vorrang: trifft ein !-Muster, gehoert der Eintrag nicht zur
// Gruppe, egal wie viele Einschluesse zutreffen. Das ist die Reihenfolge, die
// man beim Pflegen erwartet - man ergaenzt eine Ausnahme, und sie gilt.
func (g *SoftwareGroup) Matches(softwareName string) bool {
	name := strings.ToLower(softwareName)
	for _, m := range g.excludes {
		if m.matches(name) {
			return false
		}
	}
	for _, m := range g.includes {
		if m.matches(name) {
			return true
		}
	}
	return false
}

// MatchGroups liefert die Gruppen, zu denen die gemeldete Software gehoert.
//
// Eine Gruppe erscheint hoechstens einmal, auch wenn zwanzig .NET-Versionen
// installiert sind - genau darum geht es.
func MatchGroups(groups []*SoftwareGroup, software []report.Software) []*SoftwareGroup {
	var out []*SoftwareGroup
	for _, g := range groups {
		for _, sw := range software {
			if g.Matches(sw.Name) {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

// Unmatched liefert die Programmnamen, die in keine Gruppe fallen.
//
// Nicht fuer die CMDB gedacht, sondern zum Pflegen der Liste: was hier haeufig
// auftaucht, ist ein Kandidat fuer eine neue Gruppe oder ein zusaetzliches
// Muster.
func Unmatched(groups []*SoftwareGroup, software []report.Software) []string {
	var out []string
	for _, sw := range software {
		hit := false
		for _, g := range groups {
			if g.Matches(sw.Name) {
				hit = true
				break
			}
		}
		if !hit && strings.TrimSpace(sw.Name) != "" {
			out = append(out, sw.Name)
		}
	}
	return out
}

// --- Katalog aus iTop -------------------------------------------------------

// groupCache haelt den Katalog, damit nicht jede Meldung iTop abfragt.
//
// Die Liste aendert sich selten (jemand pflegt eine Gruppe nach), Meldungen
// kommen dagegen von jeder Maschine. Ohne Zwischenspeicher waere jede Meldung
// eine zusaetzliche Abfrage.
type groupCache struct {
	mu      sync.RWMutex
	groups  []*SoftwareGroup
	fetched time.Time
	ttl     time.Duration
}

func newGroupCache(ttl time.Duration) *groupCache {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &groupCache{ttl: ttl}
}

// SoftwareGroups liefert den Katalog, bei Bedarf frisch aus iTop.
func (s *Service) SoftwareGroups() ([]*SoftwareGroup, error) {
	s.groups.mu.RLock()
	if time.Since(s.groups.fetched) < s.groups.ttl && s.groups.groups != nil {
		g := s.groups.groups
		s.groups.mu.RUnlock()
		return g, nil
	}
	s.groups.mu.RUnlock()

	groups, err := s.itop.fetchSoftwareGroups(s.log)
	if err != nil {
		// Bei einem Fehler lieber den alten Stand weiterverwenden, als die
		// Zuordnung ganz ausfallen zu lassen.
		s.groups.mu.RLock()
		defer s.groups.mu.RUnlock()
		if s.groups.groups != nil {
			s.log.Warn("Software-Katalog konnte nicht aktualisiert werden, nutze letzten Stand",
				"fehler", err)
			return s.groups.groups, nil
		}
		return nil, err
	}

	s.groups.mu.Lock()
	s.groups.groups = groups
	s.groups.fetched = time.Now()
	s.groups.mu.Unlock()
	return groups, nil
}

// fetchSoftwareGroups liest alle Katalogeintraege mit Zuordnungsmustern.
func (c *ITopClient) fetchSoftwareGroups(log logger) ([]*SoftwareGroup, error) {
	res, err := c.rest(map[string]any{
		"operation": "core/get",
		"class":     "Software",
		// Nur Eintraege MIT Mustern. Software, die jemand aus anderen Gruenden
		// anlegt, soll nicht versehentlich am Abgleich teilnehmen.
		"key":           "SELECT Software WHERE agent_match_patterns != ''",
		"output_fields": "id,name,agent_match_patterns",
	})
	if err != nil {
		return nil, fmt.Errorf("Software-Katalog lesen: %w", err)
	}

	groups := make([]*SoftwareGroup, 0, len(res.Objects))
	for _, o := range res.Objects {
		id, _ := strconv.Atoi(o.Fields["id"])
		inc, exc, perr := parsePatterns(o.Fields["agent_match_patterns"])
		if perr != nil {
			// Ein kaputtes Muster darf nicht den ganzen Katalog unbrauchbar
			// machen - die betroffene Gruppe faellt aus, der Rest arbeitet weiter.
			log.Warn("Zuordnungsmuster fehlerhaft, Gruppe wird uebersprungen",
				"software_id", id, "name", o.Fields["name"], "fehler", perr)
			continue
		}
		if len(inc) == 0 {
			continue
		}
		groups = append(groups, &SoftwareGroup{
			ID: id, Name: o.Fields["name"], includes: inc, excludes: exc,
		})
	}
	return groups, nil
}

// logger ist die kleine Teilmenge von slog.Logger, die hier gebraucht wird.
// Als eigenes Interface, damit fetchSoftwareGroups ohne Service testbar bleibt.
type logger interface {
	Warn(msg string, args ...any)
}
