//go:build linux

package service

import (
	"context"
	"fmt"
)

// Name ist der Name der systemd-Unit.
const Name = "itop-agent"

// IsService gibt es unter Linux nur der Symmetrie halber.
//
// systemd startet den Prozess wie jeden anderen; es gibt keinen Unterschied
// zwischen Dienst- und Vordergrundbetrieb, den der Prozess kennen muesste.
func IsService() bool { return false }

// Run fuehrt die Arbeitsschleife aus.
//
// Das Signal-Handling (SIGTERM von systemd) sitzt in cmd/agent, weil es fuer
// beide Plattformen gleich ist.
func Run(ctx context.Context, run func(ctx context.Context)) error {
	run(ctx)
	return nil
}

// Install/Uninstall gibt es unter Linux nicht im Binary: die Unit kommt aus dem
// Paket (.deb/.rpm), nicht vom Programm selbst. Das ist dort die uebliche
// Arbeitsteilung - ein Programm, das sich selbst in systemd eintraegt, umgeht
// die Paketverwaltung.
func Install(string) error {
	return fmt.Errorf("unter Linux uebernimmt das Paket die Installation der systemd-Unit")
}

func Uninstall() error {
	return fmt.Errorf("unter Linux uebernimmt das Paket die Entfernung der systemd-Unit")
}
