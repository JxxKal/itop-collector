package collectorsrv

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ITopClient spricht mit iTop auf zwei Wegen:
//
//   - REST (webservices/rest.php) zum LESEN. Gebraucht fuer die
//     Reimaging-Aufloesung und das Auslesen von Konflikten.
//   - synchro_import.php zum SCHREIBEN. Nie core/update - die Update-Policy pro
//     Attribut steckt in der Synchro Data Source, und nur ueber den Import
//     greift sie.
type ITopClient struct {
	baseURL string
	user    string
	pwd     string
	http    *http.Client
	log     *slog.Logger
}

// ITopOptions buendelt die Verbindungsparameter.
type ITopOptions struct {
	BaseURL  string // z.B. https://itop.example.internal:8889
	User     string
	Password string

	// SkipTLSVerify schaltet die Zertifikatspruefung ab.
	//
	// Ausschliesslich fuer Testinstanzen mit selbstsigniertem Zertifikat
	// gedacht. Default ist false, und wenn es an ist, wird beim Start eine
	// Warnung geloggt - sonst wandert die Testkonfiguration unbemerkt in den
	// Pilotbetrieb.
	SkipTLSVerify bool

	Timeout time.Duration
}

// NewITopClient baut den Client. Ein leeres Timeout wird auf 3 Minuten gesetzt:
// ein synchro_import mit vielen Zeilen laeuft laenger als die ueblichen 30s.
func NewITopClient(opts ITopOptions, log *slog.Logger) *ITopClient {
	if opts.Timeout == 0 {
		opts.Timeout = 3 * time.Minute
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opts.SkipTLSVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		log.Warn("TLS-Zertifikatspruefung fuer iTop ist ABGESCHALTET",
			"hinweis", "nur fuer Testinstanzen; im Pilot- und Produktivbetrieb einschalten")
	}
	return &ITopClient{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		user:    opts.User,
		pwd:     opts.Password,
		http:    &http.Client{Transport: transport, Timeout: opts.Timeout},
		log:     log,
	}
}

// --- REST ------------------------------------------------------------------

type restResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Objects map[string]struct {
		Code    int               `json:"code"`
		Message string            `json:"message"`
		Class   string            `json:"class"`
		Key     string            `json:"key"`
		Fields  map[string]string `json:"fields"`
	} `json:"objects"`
}

// CI ist der Ausschnitt eines iTop-Objekts, den der Collector braucht.
type CI struct {
	ID        int
	Class     string
	Name      string
	AgentGUID string
	Serial    string
	OrgID     int
}

func (c *ITopClient) rest(payload map[string]any) (*restResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("json_data bauen: %w", err)
	}
	form := url.Values{
		"auth_user": {c.user},
		"auth_pwd":  {c.pwd},
		"json_data": {string(body)},
	}
	endpoint := c.baseURL + "/webservices/rest.php?version=1.3"
	resp, err := c.http.PostForm(endpoint, form)
	if err != nil {
		return nil, fmt.Errorf("REST-Aufruf: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("REST-Antwort lesen: %w", err)
	}
	var out restResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		// iTop liefert bei Konfigurationsfehlern HTML statt JSON. Den Anfang
		// mitgeben, sonst sucht man lange.
		return nil, fmt.Errorf("REST-Antwort ist kein JSON (%q...): %w",
			strings.TrimSpace(string(raw))[:min(120, len(strings.TrimSpace(string(raw))))], err)
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("iTop meldet Fehler %d: %s", out.Code, out.Message)
	}
	return &out, nil
}

// oqlEscape maskiert einfache Anfuehrungszeichen fuer OQL-Literale.
//
// Die Werte kommen aus Agent-Meldungen und sind damit potenziell
// fremdbestimmt - eine Seriennummer mit einem Apostroph darf die Abfrage nicht
// aufbrechen koennen.
func oqlEscape(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}

// query fragt eine Klasse ab.
//
// fields muss zur Klasse passen: iTop lehnt unbekannte Attributcodes mit
// "invalid attribute code" ab. serialnumber sitzt zum Beispiel erst an
// PhysicalDevice, nicht schon an FunctionalCI.
func (c *ITopClient) query(class, where, fields string) ([]CI, error) {
	res, err := c.rest(map[string]any{
		"operation":     "core/get",
		"class":         class,
		"key":           fmt.Sprintf("SELECT %s WHERE %s", class, where),
		"output_fields": fields,
	})
	if err != nil {
		return nil, err
	}
	out := make([]CI, 0, len(res.Objects))
	for _, o := range res.Objects {
		id, _ := strconv.Atoi(o.Fields["id"])
		orgID, _ := strconv.Atoi(o.Fields["org_id"])
		out = append(out, CI{
			ID:        id,
			Class:     o.Class,
			Name:      o.Fields["name"],
			AgentGUID: o.Fields["agent_guid"],
			Serial:    o.Fields["serialnumber"],
			OrgID:     orgID,
		})
	}
	return out, nil
}

// FindByAgentGUID sucht klassenuebergreifend nach einer GUID.
//
// Bewusst ueber FunctionalCI und nicht ueber die Zielklasse: in M0 hat sich
// gezeigt, dass die Reconciliation einer Datasource auf ihre scope_class
// beschraenkt ist. Wird dieselbe Maschine einmal als PC und einmal als Server
// gemeldet, entstehen zwei CIs, ohne dass iTop das bemerkt. Der Collector muss
// also selbst ueber alle Klassen schauen.
func (c *ITopClient) FindByAgentGUID(guid string) ([]CI, error) {
	if guid == "" {
		return nil, nil
	}
	// serialnumber ist hier NICHT abfragbar - das Attribut sitzt erst an
	// PhysicalDevice. Fuer die Existenzpruefung wird es auch nicht gebraucht.
	return c.query("FunctionalCI", fmt.Sprintf("agent_guid = '%s'", oqlEscape(guid)),
		"id,name,agent_guid,org_id")
}

// FindBySerial sucht klassenuebergreifend nach einer Seriennummer.
//
// Grundlage der Reimaging-Aufloesung. Ein leerer Wert wird NIE gesucht - der
// Agent liefert bei OEM-Platzhaltern eine leere Seriennummer, und die wuerde
// sonst auf jedes CI ohne Seriennummer passen.
func (c *ITopClient) FindBySerial(serial string) ([]CI, error) {
	if strings.TrimSpace(serial) == "" {
		return nil, nil
	}
	return c.query("PhysicalDevice", fmt.Sprintf("serialnumber = '%s'", oqlEscape(serial)),
		"id,name,agent_guid,serialnumber")
}

// --- synchro_import --------------------------------------------------------

// ImportResult fasst zusammen, was ein Import bewirkt hat.
type ImportResult struct {
	Created         int
	Updated         int
	Unchanged       int
	CreationErrors  int
	ReconcileErrors int
	Raw             string
}

// HasErrors sagt, ob der Import Nacharbeit braucht.
//
// ACHTUNG: iTop zaehlt in der Zusammenfassung ueber ALLE Replicas der Data
// Source, nicht nur ueber die gerade gelieferten Zeilen. Ein alter, nie
// aufgeloester Replica laesst deshalb jede spaetere Meldung als fehlerhaft
// erscheinen, obwohl sie sauber durchgelaufen ist.
//
// Die Angabe taugt daher als Hinweis auf den Zustand der Data Source, nicht als
// Urteil ueber die einzelne Meldung. Fuer eine genaue Aussage muesste der
// Collector nach dem Import den Replica zu diesem primary_key gezielt nachlesen.
func (r ImportResult) HasErrors() bool {
	return r.CreationErrors > 0 || r.ReconcileErrors > 0
}

// Die Zusammenfassung sieht ueber HTTP so aus (mit fuehrenden Leerzeichen VOR
// dem Doppelkreuz und einem <br/> am Ende):
//
//	#Objects created: 1 (0 warnings)<br/>
//
// Deshalb \s* auch vor dem '#'. Ohne das zaehlt der Parser stumm nur Nullen,
// obwohl der Import funktioniert hat.
var summaryLine = regexp.MustCompile(`(?m)^\s*#?\s*(Objects created|Objects updated|Objects reconciled \(unchanged\)|Objects creation errors|Objects reconciliation errors):\s*(\d+)`)

// SynchroImport schiebt CSV-Daten in eine Synchro Data Source.
//
// synchronize=1 laesst iTop direkt im Anschluss die Synchronisationsphase
// laufen. Ohne das lagen die Replicas nur herum, bis irgendwann der Cron kommt.
func (c *ITopClient) SynchroImport(dataSourceID int, csv string) (ImportResult, error) {
	form := url.Values{
		"auth_user":      {c.user},
		"auth_pwd":       {c.pwd},
		"data_source_id": {strconv.Itoa(dataSourceID)},
		"separator":      {";"},
		"charset":        {"UTF-8"},
		"date_format":    {"Y-m-d H:i:s"},
		"output":         {"summary"},
		"synchronize":    {"1"},
		"csvdata":        {csv},
		"comment":        {"itop-agent collector"},
	}
	resp, err := c.http.PostForm(c.baseURL+"/synchro/synchro_import.php", form)
	if err != nil {
		return ImportResult{}, fmt.Errorf("synchro_import aufrufen: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ImportResult{}, fmt.Errorf("synchro_import-Antwort lesen: %w", err)
	}
	text := string(raw)

	res := ImportResult{Raw: text}
	for _, m := range summaryLine.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[2])
		switch m[1] {
		case "Objects created":
			res.Created = n
		case "Objects updated":
			res.Updated = n
		case "Objects reconciled (unchanged)":
			res.Unchanged = n
		case "Objects creation errors":
			res.CreationErrors = n
		case "Objects reconciliation errors":
			res.ReconcileErrors = n
		}
	}
	// Findet der Parser gar nichts, hat iTop keine Zusammenfassung geliefert -
	// typischerweise eine HTML-Fehlerseite. Das darf nicht als Erfolg durchgehen.
	if !strings.Contains(text, "Import phase summary") {
		return res, fmt.Errorf("synchro_import lieferte keine Zusammenfassung: %s",
			strings.TrimSpace(text)[:min(200, len(strings.TrimSpace(text)))])
	}
	return res, nil
}

// PendingConflicts liest Replicas, die nicht zugeordnet werden konnten.
//
// Das ist die Datenquelle fuer das Konfliktlogging: bei action_on_multiple=error
// bleibt der Replica auf status='new' stehen und traegt den Grund in
// status_last_error - zum Beispiel "2 destination objects match the
// reconciliation criterias: agent_guid=...".
func (c *ITopClient) PendingConflicts() ([]Conflict, error) {
	res, err := c.rest(map[string]any{
		"operation":     "core/get",
		"class":         "SynchroReplica",
		"key":           "SELECT SynchroReplica WHERE status = 'new' AND status_last_error != ''",
		"output_fields": "id,status,status_last_error",
	})
	if err != nil {
		return nil, err
	}
	out := make([]Conflict, 0, len(res.Objects))
	for _, o := range res.Objects {
		id, _ := strconv.Atoi(o.Fields["id"])
		out = append(out, Conflict{ReplicaID: id, Error: o.Fields["status_last_error"]})
	}
	return out, nil
}

// Conflict ist ein Replica, den iTop nicht zuordnen konnte.
type Conflict struct {
	ReplicaID int
	Error     string
}
