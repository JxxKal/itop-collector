//go:build windows

package service

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/sys/windows/svc/eventlog"
)

// Ein Windows-Dienst hat kein Terminal: alles, was nach stdout oder stderr
// geht, ist verloren. Ohne Eventlog laeuft der Agent als Dienst also blind -
// man sieht, DASS er laeuft, aber nicht, ob er meldet oder seit Tagen an einem
// Zertifikatsfehler scheitert.
//
// Deshalb bekommt slog im Dienstbetrieb ein Ziel, das ins Anwendungsprotokoll
// schreibt. Interaktiv bleibt es bei stderr - dort ist das Terminal ja da.

// eventID unterscheidet die Meldungen im Protokoll. Windows fuehrt sie als
// Zahl; ohne eine mitgelieferte Nachrichtendatei zeigt die Ereignisanzeige
// zusaetzlich einen Hinweis auf die fehlende Beschreibung an. Das ist bekannt
// und stoert die Lesbarkeit des Textes nicht.
const eventID = 1

// Install registriert die Ereignisquelle mit.
func registerEventSource() error {
	// Bereits vorhandene Quelle ist kein Fehler - nur beim ersten Mal noetig.
	err := eventlog.InstallAsEventCreate(Name, eventlog.Info|eventlog.Warning|eventlog.Error)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("Ereignisquelle registrieren: %w", err)
	}
	return nil
}

func removeEventSource() error {
	if err := eventlog.Remove(Name); err != nil && !strings.Contains(err.Error(), "cannot find") {
		return fmt.Errorf("Ereignisquelle entfernen: %w", err)
	}
	return nil
}

// eventLogWriter leitet Logzeilen ins Anwendungsprotokoll.
type eventLogWriter struct{ log *eventlog.Log }

// Write ordnet jede Zeile einer Schwere zu.
//
// slog schreibt eine Zeile je Ereignis; die Stufe steht als "level=..." darin.
// Das auszuwerten ist einfacher und robuster, als slog einen eigenen Handler
// unterzuschieben - und die Ereignisanzeige kann danach filtern.
func (w *eventLogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\r\n")
	if msg == "" {
		return len(p), nil
	}
	var err error
	switch {
	case strings.Contains(msg, "level=ERROR"):
		err = w.log.Error(eventID, msg)
	case strings.Contains(msg, "level=WARN"):
		err = w.log.Warning(eventID, msg)
	default:
		err = w.log.Info(eventID, msg)
	}
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// LogWriter liefert das Ziel fuer slog.
//
// Im Dienstbetrieb das Eventlog, sonst das uebergebene Ziel (stderr). Der
// zweite Rueckgabewert schliesst das Protokoll wieder.
func LogWriter(fallback io.Writer) (io.Writer, func()) {
	if !IsService() {
		return fallback, func() {}
	}
	el, err := eventlog.Open(Name)
	if err != nil {
		// Nicht registrierte Quelle: lieber weiterlaufen und nach stderr
		// schreiben, als den Dienst wegen des Protokolls nicht zu starten.
		return fallback, func() {}
	}
	return &eventLogWriter{log: el}, func() { _ = el.Close() }
}
