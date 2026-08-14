// Command collector nimmt Agent-Meldungen entgegen und schreibt sie ueber
// iTop Synchro Data Sources in die CMDB.
//
// Konfiguration ausschliesslich ueber Umgebungsvariablen - der Dienst laeuft im
// Container, und Variablen sind dort der Weg, auf dem Compose, Portainer und
// Secrets ohnehin liefern.
package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/JxxKal/itop-collector/internal/collectorsrv"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string) bool {
	v := strings.ToLower(os.Getenv(key))
	return v == "1" || v == "true" || v == "yes"
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := collectorsrv.Config{
		Listen:       env("COLLECTOR_LISTEN", ":8080"),
		EnrollToken:  os.Getenv("COLLECTOR_ENROLL_TOKEN"),
		RegistryPath: env("COLLECTOR_REGISTRY", "/var/lib/itop-collector/devices.json"),
		DefaultOrgID: env("ITOP_DEFAULT_ORG_ID", ""),
		DataSources: map[collectorsrv.TargetClass]int{
			collectorsrv.ClassPC:             envInt("ITOP_DS_PC", 0),
			collectorsrv.ClassServer:         envInt("ITOP_DS_SERVER", 0),
			collectorsrv.ClassVirtualMachine: envInt("ITOP_DS_VM", 0),
		},
		ITop: collectorsrv.ITopOptions{
			BaseURL:  os.Getenv("ITOP_URL"),
			User:     os.Getenv("ITOP_USER"),
			Password: os.Getenv("ITOP_PASSWORD"),
			// Schalter fuer Testinstanzen mit selbstsigniertem Zertifikat.
			// Default aus; wenn an, warnt der Client beim Start.
			SkipTLSVerify: envBool("ITOP_SKIP_TLS_VERIFY"),
			Timeout:       3 * time.Minute,
		},
	}

	svc, err := collectorsrv.New(cfg, log)
	if err != nil {
		log.Error("Start fehlgeschlagen", "fehler", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           svc.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Info("Collector startet", "listen", cfg.Listen, "itop", cfg.ITop.BaseURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("Server beendet", "fehler", err)
		os.Exit(1)
	}
}
