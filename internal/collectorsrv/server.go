package collectorsrv

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/JxxKal/itop-collector/internal/report"
)

// Config buendelt alles, was der Collector zum Laufen braucht.
type Config struct {
	Listen string // z.B. ":8080"

	// EnrollToken ist das Einmal-Token, das der Installer mitbringt. Ein Agent
	// tauscht es zusammen mit seiner GUID gegen ein geraetespezifisches Token.
	EnrollToken string

	RegistryPath string

	// DefaultOrgID ist die iTop-Organisation, unter der neue CIs angelegt
	// werden. Nur bei der Anlage wirksam (write_if_empty).
	DefaultOrgID string

	// DataSources bildet Zielklasse -> Synchro-Data-Source-ID ab.
	DataSources map[TargetClass]int

	ITop ITopOptions
}

// Service ist der Collector.
type Service struct {
	cfg  Config
	reg  *Registry
	itop *ITopClient
	refs *refCache
	log  *slog.Logger
}

// New baut den Service.
func New(cfg Config, log *slog.Logger) (*Service, error) {
	if cfg.EnrollToken == "" {
		return nil, errors.New("EnrollToken fehlt - ohne Einmal-Token kann sich kein Agent registrieren")
	}
	if cfg.DefaultOrgID == "" {
		return nil, errors.New("DefaultOrgID fehlt - neue CIs brauchen eine Organisation")
	}
	reg, err := LoadRegistry(cfg.RegistryPath)
	if err != nil {
		return nil, err
	}
	log.Info("Registry geladen", "pfad", cfg.RegistryPath, "geraete", reg.Count())
	return &Service{
		cfg:  cfg,
		reg:  reg,
		itop: NewITopClient(cfg.ITop, log),
		refs: newRefCache(),
		log:  log,
	}, nil
}

// Routes liefert den HTTP-Handler.
func (s *Service) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /enroll", s.handleEnroll)
	mux.HandleFunc("POST /report", s.handleReport)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /conflicts", s.handleConflicts)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// bearer liest das Token aus dem Authorization-Header.
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// --- /enroll ---------------------------------------------------------------

type enrollRequest struct {
	AgentGUID string `json:"agent_guid"`
}

type enrollResponse struct {
	DeviceToken string `json:"device_token"`
}

func (s *Service) handleEnroll(w http.ResponseWriter, r *http.Request) {
	// Das Einmal-Token wird in konstanter Zeit verglichen, damit es sich nicht
	// zeichenweise erraten laesst.
	if !constantEquals(bearer(r), s.cfg.EnrollToken) {
		s.log.Warn("Enrollment mit falschem Einmal-Token abgewiesen", "remote", r.RemoteAddr)
		writeErr(w, http.StatusUnauthorized, "ungueltiges Enrollment-Token")
		return
	}
	var req enrollRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "ungueltiges JSON")
		return
	}
	if strings.TrimSpace(req.AgentGUID) == "" {
		writeErr(w, http.StatusBadRequest, "agent_guid fehlt")
		return
	}
	token, dev, err := s.reg.Enroll(req.AgentGUID)
	if err != nil {
		s.log.Error("Enrollment fehlgeschlagen", "agent_guid", req.AgentGUID, "fehler", err)
		writeErr(w, http.StatusInternalServerError, "Enrollment fehlgeschlagen")
		return
	}
	s.log.Info("Geraet registriert", "agent_guid", dev.AgentGUID, "klasse", dev.TargetClass)
	writeJSON(w, http.StatusOK, enrollResponse{DeviceToken: token})
}

// --- /report ---------------------------------------------------------------

type reportResponse struct {
	Status      string `json:"status"`
	TargetClass string `json:"target_class"`
	UsedGUID    string `json:"used_agent_guid"`
	Created     int    `json:"created"`
	Updated     int    `json:"updated"`
	Unchanged   int    `json:"unchanged"`
}

func (s *Service) handleReport(w http.ResponseWriter, r *http.Request) {
	var rep report.Report
	// 8 MiB: ein Software-Inventar mit einigen tausend Eintraegen passt bequem
	// hinein, ein versehentlicher Dauerstrom nicht.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&rep); err != nil {
		writeErr(w, http.StatusBadRequest, "ungueltiges JSON")
		return
	}
	if strings.TrimSpace(rep.AgentGUID) == "" {
		writeErr(w, http.StatusBadRequest, "agent_guid fehlt")
		return
	}
	dev, err := s.reg.Authenticate(rep.AgentGUID, bearer(r))
	if err != nil {
		s.log.Warn("Meldung abgewiesen", "agent_guid", rep.AgentGUID, "remote", r.RemoteAddr)
		writeErr(w, http.StatusUnauthorized, "unbekanntes Geraet oder falsches Token")
		return
	}
	if rep.CollectedAt.IsZero() {
		rep.CollectedAt = time.Now().UTC()
	}

	res, err := s.Ingest(dev, &rep)
	if err != nil {
		// Ein mehrdeutiger Treffer ist kein Serverfehler, sondern ein Zustand,
		// den ein Mensch aufloesen muss. Der Agent soll nicht endlos wiederholen.
		if errors.Is(err, ErrAmbiguousSerial) || errors.Is(err, ErrVMNeedsHypervisor) {
			s.log.Warn("Meldung nicht verarbeitbar", "agent_guid", rep.AgentGUID, "grund", err)
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		s.log.Error("Meldung fehlgeschlagen", "agent_guid", rep.AgentGUID, "fehler", err)
		writeErr(w, http.StatusBadGateway, "iTop nicht erreichbar oder Import fehlgeschlagen")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// Ingest verarbeitet eine Meldung: Klasse bestimmen, Reimaging aufloesen,
// CSV bauen, an die passende Datasource schicken.
func (s *Service) Ingest(dev *Device, rep *report.Report) (reportResponse, error) {
	cls := s.ResolveClass(dev, rep)

	guid, err := s.ResolveReimaging(rep)
	if err != nil {
		return reportResponse{}, err
	}

	// VirtualMachine kann der Agent nicht anlegen - virtualhost_id ist Pflicht
	// und dem Geraet unbekannt. Nur ergaenzen, was es schon gibt.
	if cls == ClassVirtualMachine {
		known, err := s.itop.FindByAgentGUID(guid)
		if err != nil {
			return reportResponse{}, err
		}
		if len(known) == 0 {
			// Die Hypervisor-Quelle setzt kein agent_guid, und VirtualMachine hat
			// keine serialnumber - ohne Uebernahme ueber den Namen bliebe die VM
			// dauerhaft unerreichbar. Siehe adopt.go.
			adopted, err := s.AdoptVirtualMachine(guid, rep.Hostname)
			if err != nil {
				return reportResponse{}, err
			}
			if !adopted {
				return reportResponse{}, fmt.Errorf("%w: agent_guid=%s hostname=%s",
					ErrVMNeedsHypervisor, guid, rep.Hostname)
			}
		}
	}

	// Fremdschluessel-Ziele anlegen, bevor die CSV sie referenziert. Sonst
	// scheitert der Import mit "No result for the single row query".
	if err := s.EnsureOSRefs(rep.OSName, rep.OSVersion); err != nil {
		return reportResponse{}, err
	}

	dsID, ok := s.cfg.DataSources[cls]
	if !ok {
		return reportResponse{}, fmt.Errorf("keine Synchro Data Source fuer Zielklasse %q konfiguriert", cls)
	}

	csv, err := BuildCSV(cls, []CSVRow{{Report: rep, GUID: guid, OrgID: s.cfg.DefaultOrgID}})
	if err != nil {
		return reportResponse{}, err
	}
	imp, err := s.itop.SynchroImport(dsID, csv)
	if err != nil {
		return reportResponse{}, err
	}
	if imp.HasErrors() {
		// Die Zaehler gelten fuer die gesamte Data Source, nicht nur fuer diese
		// Meldung - siehe ImportResult.HasErrors. Deshalb als Zustandshinweis
		// formuliert und nicht als Fehler dieser Meldung.
		s.log.Warn("Data Source enthaelt unaufgeloeste Replicas",
			"agent_guid", guid, "klasse", cls,
			"creation_errors", imp.CreationErrors, "reconcile_errors", imp.ReconcileErrors,
			"hinweis", "kann von aelteren Meldungen stammen; /conflicts zeigt die Liste")
	}
	// Primaere IP nach dem Import setzen: sie braucht das CI, und das entsteht
	// erst durch den Import. Ein Fehler hier darf die Meldung nicht scheitern
	// lassen - die Inventardaten sind bereits in iTop.
	if err := s.SyncPrimaryIP(cls, guid, rep.PrimaryIP(), s.cfg.DefaultOrgID); err != nil {
		s.log.Warn("primaere IP konnte nicht gesetzt werden",
			"agent_guid", guid, "ip", rep.PrimaryIP(), "fehler", err)
	}

	if err := s.reg.Touch(dev); err != nil {
		// Die Daten sind in iTop - ein Registry-Schreibfehler darf die Meldung
		// nicht scheitern lassen, muss aber auffallen.
		s.log.Error("Registry konnte nicht aktualisiert werden", "fehler", err)
	}
	return reportResponse{
		Status:      "ok",
		TargetClass: string(cls),
		UsedGUID:    guid,
		Created:     imp.Created,
		Updated:     imp.Updated,
		Unchanged:   imp.Unchanged,
	}, nil
}

// --- /healthz, /conflicts --------------------------------------------------

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "devices": s.reg.Count()})
}

// handleConflicts zeigt Replicas, die iTop nicht zuordnen konnte - die Liste,
// die ein Mensch abarbeiten muss.
func (s *Service) handleConflicts(w http.ResponseWriter, r *http.Request) {
	if !constantEquals(bearer(r), s.cfg.EnrollToken) {
		writeErr(w, http.StatusUnauthorized, "nicht berechtigt")
		return
	}
	conflicts, err := s.itop.PendingConflicts()
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(conflicts), "conflicts": conflicts})
}
