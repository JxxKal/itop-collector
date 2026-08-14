// Command agent sammelt Inventardaten und meldet sie an den Collector.
//
// Aufrufarten:
//
//	itop-agent                 Dienstbetrieb: getaktet melden
//	itop-agent -collect        einmal sammeln, JSON auf stdout, nichts senden
//	itop-agent -enroll TOKEN   Einmal-Token gegen Device-Token tauschen
//	itop-agent -once           einmal sammeln und sofort senden
//	itop-agent -install        als Windows-Dienst registrieren
//	itop-agent -uninstall      Windows-Dienst entfernen
//
// -collect ist der Modus fuer Entwicklung und Support: er braucht weder
// Collector noch Enrollment und zeigt genau das, was gemeldet wuerde.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JxxKal/itop-collector/internal/collect"
	"github.com/JxxKal/itop-collector/internal/config"
	"github.com/JxxKal/itop-collector/internal/identity"
	"github.com/JxxKal/itop-collector/internal/push"
	"github.com/JxxKal/itop-collector/internal/sched"
	"github.com/JxxKal/itop-collector/internal/service"
)

// Version wird beim Bauen gesetzt: -ldflags "-X main.Version=1.2.3"
var Version = "0.1.0-dev"

func main() {
	var (
		collectOnly = flag.Bool("collect", false, "einmal sammeln und als JSON ausgeben, nichts senden")
		once        = flag.Bool("once", false, "einmal sammeln und sofort senden")
		enrollToken = flag.String("enroll", "", "Einmal-Token gegen ein Device-Token tauschen")
		setURL      = flag.String("set-url", "", "Collector-URL dauerhaft hinterlegen (Windows: Registry)")
		install     = flag.Bool("install", false, "als Systemdienst registrieren (Windows)")
		uninstall   = flag.Bool("uninstall", false, "Systemdienst entfernen (Windows)")
	)
	flag.Parse()

	// Im Dienstbetrieb geht die Ausgabe ins Windows-Eventlog bzw. unter Linux
	// nach stderr und damit ins Journal. Interaktiv immer nach stderr, damit
	// stdout dem JSON von -collect gehoert.
	logDst, closeLog := service.LogWriter(os.Stderr)
	defer closeLog()
	log := slog.New(slog.NewTextHandler(logDst, &slog.HandlerOptions{Level: slog.LevelInfo}))

	switch {
	case *setURL != "":
		if err := config.Set(config.KeyCollectorURL, *setURL); err != nil {
			log.Error("Collector-URL konnte nicht hinterlegt werden", "fehler", err)
			os.Exit(1)
		}
		log.Info("Collector-URL hinterlegt", "url", *setURL)
		return
	case *install:
		exe, err := os.Executable()
		if err != nil {
			log.Error("eigenen Pfad ermitteln", "fehler", err)
			os.Exit(1)
		}
		if err := service.Install(exe); err != nil {
			log.Error("Dienst konnte nicht installiert werden", "fehler", err)
			os.Exit(1)
		}
		log.Info("Dienst installiert", "name", service.Name, "pfad", exe)
		return
	case *uninstall:
		if err := service.Uninstall(); err != nil {
			log.Error("Dienst konnte nicht entfernt werden", "fehler", err)
			os.Exit(1)
		}
		log.Info("Dienst entfernt", "name", service.Name)
		return
	}

	// -collect braucht nichts weiter: keine Konfiguration, kein Netz, keine
	// dauerhafte GUID. So laesst sich auf jeder Maschine sofort pruefen, was der
	// Agent ueberhaupt sieht.
	if *collectOnly {
		guid, err := identity.AgentGUID()
		if err != nil {
			// Ohne Schreibrechte auf /var/lib gibt es keine dauerhafte GUID.
			// Fuer die reine Anzeige ist das kein Grund abzubrechen.
			log.Warn("keine dauerhafte GUID verfuegbar - Ausgabe mit Platzhalter",
				"grund", err)
			guid = "(nicht gespeichert)"
		}
		rep := collect.Collect(guid, Version)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			log.Error("Ausgabe fehlgeschlagen", "fehler", err)
			os.Exit(1)
		}
		return
	}

	// Umgebungsvariable zuerst, dann der plattformeigene Speicher. Ein
	// Windows-Dienst erbt die Umgebung der Shell nicht - ohne den zweiten Ort
	// startet er und steigt sofort wieder aus.
	collectorURL := config.Get(config.KeyCollectorURL, "")
	if collectorURL == "" {
		log.Error("Collector-URL fehlt",
			"hinweis", "ITOP_COLLECTOR_URL setzen oder mit -set-url dauerhaft hinterlegen")
		os.Exit(1)
	}
	caCert := config.Get(config.KeyCACert, "")
	skipTLS := config.Get(config.KeySkipTLSVerify, "") == "1"

	guid, err := identity.AgentGUID()
	if err != nil {
		log.Error("Agent-GUID konnte nicht ermittelt werden", "fehler", err)
		os.Exit(1)
	}

	client, err := push.New(push.Options{
		CollectorURL:  collectorURL,
		CACertPath:    caCert,
		SkipTLSVerify: skipTLS,
	}, log)
	if err != nil {
		log.Error("Client konnte nicht aufgebaut werden", "fehler", err)
		os.Exit(1)
	}

	if *enrollToken != "" {
		token, err := client.Enroll(guid, *enrollToken)
		if err != nil {
			log.Error("Enrollment fehlgeschlagen", "fehler", err)
			os.Exit(1)
		}
		if err := identity.SaveDeviceToken(token); err != nil {
			log.Error("Device-Token konnte nicht gespeichert werden", "fehler", err)
			os.Exit(1)
		}
		log.Info("Enrollment erfolgreich", "agent_guid", guid)
		return
	}

	token, err := identity.DeviceToken()
	if err != nil {
		log.Error("kein Device-Token vorhanden - zuerst mit -enroll registrieren", "fehler", err)
		os.Exit(1)
	}
	client, err = push.New(push.Options{
		CollectorURL:  collectorURL,
		DeviceToken:   token,
		CACertPath:    caCert,
		SkipTLSVerify: skipTLS,
	}, log)
	if err != nil {
		log.Error("Client konnte nicht aufgebaut werden", "fehler", err)
		os.Exit(1)
	}

	report := func() {
		start := time.Now()
		rep := collect.Collect(guid, Version)
		if err := client.Send(rep); err != nil {
			log.Error("Meldung endgueltig fehlgeschlagen", "fehler", err)
			return
		}
		log.Info("gemeldet", "hostname", rep.Hostname, "dauer", time.Since(start).Round(time.Millisecond))
	}

	if *once {
		report()
		return
	}

	// SIGTERM sauber behandeln: systemd schickt es beim Stoppen, und ein
	// laufender Sammellauf soll dabei nicht mitten im Schreiben abbrechen.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	opts := sched.Defaults()
	log.Info("Agent gestartet", "version", Version, "agent_guid", guid,
		"collector", collectorURL, "intervall", opts.Interval.String(),
		"als_dienst", service.IsService())

	// Unter Windows meldet sich der Prozess hier beim Service Control Manager
	// an; unter Linux ruft service.Run die Schleife direkt auf.
	if err := service.Run(ctx, func(ctx context.Context) {
		sched.Run(ctx, opts, report)
	}); err != nil {
		log.Error("Dienstbetrieb fehlgeschlagen", "fehler", err)
		os.Exit(1)
	}
	log.Info("Agent beendet")
}
