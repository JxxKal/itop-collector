// Package push schickt Meldungen an den Collector.
package push

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/JxxKal/itop-collector/internal/report"
)

// Options konfiguriert den Client.
type Options struct {
	CollectorURL string
	DeviceToken  string

	// CACertPath ist die interne CA. Nicht-domaenengebundene Maschinen bekommen
	// keine Root-CA per GPO, deshalb bringt der Agent sie selbst mit.
	CACertPath string

	// SkipTLSVerify schaltet die Zertifikatspruefung ab.
	//
	// AUSSCHLIESSLICH fuer Testaufbauten. Default aus; wenn an, wird bei jedem
	// Start gewarnt - sonst wandert die Testkonfiguration unbemerkt in den
	// Pilotbetrieb.
	SkipTLSVerify bool

	Timeout time.Duration
}

// Client schickt Reports.
type Client struct {
	opts Options
	http *http.Client
	log  *slog.Logger
}

// New baut den Client und richtet den Vertrauensanker ein.
func New(opts Options, log *slog.Logger) (*Client, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	switch {
	case opts.SkipTLSVerify:
		tlsCfg.InsecureSkipVerify = true
		log.Warn("TLS-Zertifikatspruefung ist ABGESCHALTET",
			"hinweis", "nur fuer Testaufbauten; vor dem Pilotbetrieb einschalten")
	case opts.CACertPath != "":
		pem, err := os.ReadFile(opts.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("CA-Zertifikat lesen: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA-Zertifikat %s enthaelt kein gueltiges PEM", opts.CACertPath)
		}
		// Nur diese CA gilt - das ist das Pinning aus Abschnitt 6.
		tlsCfg.RootCAs = pool
		log.Info("interne CA verankert", "pfad", opts.CACertPath)
	}
	transport.TLSClientConfig = tlsCfg

	return &Client{
		opts: opts,
		http: &http.Client{Transport: transport, Timeout: opts.Timeout},
		log:  log,
	}, nil
}

// Enroll tauscht das Einmal-Token gegen ein Device-Token.
func (c *Client) Enroll(agentGUID, enrollToken string) (string, error) {
	body, _ := json.Marshal(map[string]string{"agent_guid": agentGUID})
	req, err := http.NewRequest(http.MethodPost, c.opts.CollectorURL+"/enroll", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+enrollToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("Enrollment: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Enrollment abgelehnt (HTTP %d): %s", resp.StatusCode, string(raw))
	}
	var out struct {
		DeviceToken string `json:"device_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("Enrollment-Antwort auswerten: %w", err)
	}
	if out.DeviceToken == "" {
		return "", fmt.Errorf("Enrollment lieferte kein Token")
	}
	return out.DeviceToken, nil
}

// ErrPermanent kennzeichnet Fehler, bei denen ein Wiederholen nichts bringt.
//
// Ein Konflikt (409, z.B. mehrdeutige Seriennummer oder unbekannte VM) muss ein
// Mensch aufloesen. Der Agent soll das nicht alle vier Minuten erneut versuchen.
type ErrPermanent struct{ Msg string }

func (e *ErrPermanent) Error() string { return e.Msg }

// Send schickt einen Report mit Wiederholungen und wachsendem Abstand.
//
// 3 Versuche, 1 -> 4 -> 16 Minuten. Der Agent stuerzt dabei nie ab; er
// protokolliert und laeuft weiter.
func (c *Client) Send(rep *report.Report) error {
	backoff := time.Minute
	var lastErr error

	for attempt := 1; attempt <= 3; attempt++ {
		err := c.sendOnce(rep)
		if err == nil {
			return nil
		}
		var perm *ErrPermanent
		if ok := asPermanent(err, &perm); ok {
			return err
		}
		lastErr = err
		c.log.Warn("Meldung fehlgeschlagen", "versuch", attempt, "fehler", err,
			"naechster_versuch_in", backoff.String())
		if attempt < 3 {
			time.Sleep(backoff)
			backoff *= 4
		}
	}
	return lastErr
}

func asPermanent(err error, target **ErrPermanent) bool {
	p, ok := err.(*ErrPermanent)
	if ok {
		*target = p
	}
	return ok
}

func (c *Client) sendOnce(rep *report.Report) error {
	body, err := json.Marshal(rep)
	if err != nil {
		return fmt.Errorf("Report serialisieren: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.opts.CollectorURL+"/report", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.opts.DeviceToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Verbindung zum Collector: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	// 4xx ausser 429 sind Zustaende, die sich durch Wiederholen nicht aendern.
	case resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests:
		return &ErrPermanent{Msg: fmt.Sprintf("Collector lehnt ab (HTTP %d): %s", resp.StatusCode, string(raw))}
	default:
		return fmt.Errorf("Collector antwortete HTTP %d: %s", resp.StatusCode, string(raw))
	}
}
