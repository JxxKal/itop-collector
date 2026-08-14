//go:build windows

// Package service kapselt den Betrieb als Systemdienst.
//
// Die Plattformen unterscheiden sich hier grundlegend: Windows verlangt, dass
// der Prozess sich beim Service Control Manager meldet und auf dessen Befehle
// antwortet; unter Linux genuegt sauberes Signal-Handling. Nach aussen bieten
// beide dieselbe Funktion Run(), damit cmd/agent nichts davon wissen muss.
package service

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Name ist der Dienstname im Service Control Manager.
const Name = "itop-agent"

type agentService struct {
	run func(ctx context.Context)
}

// Execute bedient den Service Control Manager.
//
// Wichtig ist die schnelle erste Statusmeldung: meldet sich ein Dienst nicht
// binnen kurzer Zeit als gestartet, bricht Windows den Start ab. Die eigentliche
// Arbeit laeuft deshalb in einer eigenen Goroutine.
func (s *agentService) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.run(ctx)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		case <-done:
			// Die Arbeitsschleife hat von sich aus geendet.
			return false, 0
		}
	}
}

// IsService sagt, ob der Prozess vom Service Control Manager gestartet wurde.
func IsService() bool {
	isSvc, err := svc.IsWindowsService()
	return err == nil && isSvc
}

// Run startet die Arbeitsschleife - als Dienst oder direkt.
func Run(ctx context.Context, run func(ctx context.Context)) error {
	if IsService() {
		return svc.Run(Name, &agentService{run: run})
	}
	run(ctx)
	return nil
}

// Install registriert den Dienst.
func Install(exePath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("Service Control Manager: %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(Name); err == nil {
		s.Close()
		return fmt.Errorf("Dienst %s ist bereits installiert", Name)
	}
	// Ereignisquelle mitregistrieren, sonst kann der Dienst spaeter nicht ins
	// Anwendungsprotokoll schreiben - und liefe damit unbeobachtbar.
	if err := registerEventSource(); err != nil {
		return err
	}
	s, err := m.CreateService(Name, exePath, mgr.Config{
		DisplayName: "iTop Inventory Agent",
		Description: "Sammelt Inventardaten und meldet sie an den iTop-Collector.",
		// Automatisch starten, aber verzoegert: beim Systemstart ist das Netz
		// oft noch nicht bereit, und der Agent wartet ohnehin ein paar Minuten.
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
	})
	if err != nil {
		return fmt.Errorf("Dienst anlegen: %w", err)
	}
	defer s.Close()
	return nil
}

// Uninstall entfernt den Dienst.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("Service Control Manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("Dienst %s nicht gefunden: %w", Name, err)
	}
	defer s.Close()
	if err := s.Delete(); err != nil {
		return fmt.Errorf("Dienst entfernen: %w", err)
	}
	return removeEventSource()
}
