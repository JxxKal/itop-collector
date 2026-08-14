// Package sched taktet die Meldungen des Agents.
//
// Der Takt liegt bewusst IM AGENT und nicht in systemd-Timern oder der
// Windows-Aufgabenplanung: so verhalten sich beide Plattformen identisch und es
// gibt nur eine Logik zu testen.
package sched

import (
	"context"
	"math/rand"
	"time"
)

// Options steuert den Takt.
type Options struct {
	// Interval ist der Grundabstand zwischen zwei Meldungen (Vorgabe 24h).
	Interval time.Duration

	// Jitter ist die Obergrenze eines zufaelligen Zuschlags. Ohne ihn melden
	// sich alle Maschinen, die gemeinsam ausgerollt oder nach einem Stromausfall
	// gemeinsam gestartet wurden, im Gleichtakt - und der Collector bekommt
	// einmal am Tag die gesamte Flotte auf einmal.
	Jitter time.Duration

	// StartDelayMin/Max verzoegern die erste Meldung nach dem Systemstart.
	// Beim Booten ist das Netz oft noch nicht da; ein sofortiger Versuch
	// scheitert und verbraucht nur einen Wiederholungsversuch.
	StartDelayMin time.Duration
	StartDelayMax time.Duration
}

// Defaults entsprechen Abschnitt 7 des PROJECT.md.
func Defaults() Options {
	return Options{
		Interval:      24 * time.Hour,
		Jitter:        30 * time.Minute,
		StartDelayMin: 2 * time.Minute,
		StartDelayMax: 7 * time.Minute,
	}
}

// randDuration liefert einen Wert aus [0, max).
func randDuration(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(max)))
}

// NextDelay liefert den Abstand bis zur naechsten Meldung.
func (o Options) NextDelay() time.Duration {
	return o.Interval + randDuration(o.Jitter)
}

// StartDelay liefert die Verzoegerung der ersten Meldung.
func (o Options) StartDelay() time.Duration {
	if o.StartDelayMax <= o.StartDelayMin {
		return o.StartDelayMin
	}
	return o.StartDelayMin + randDuration(o.StartDelayMax-o.StartDelayMin)
}

// Run ruft fn im konfigurierten Takt auf, bis ctx endet.
//
// fn darf fehlschlagen - der Takt laeuft weiter. Ein Fehler beim Melden ist ein
// Ereignis, kein Grund, den Dienst zu beenden.
func Run(ctx context.Context, o Options, fn func()) {
	select {
	case <-time.After(o.StartDelay()):
	case <-ctx.Done():
		return
	}
	for {
		fn()
		select {
		case <-time.After(o.NextDelay()):
		case <-ctx.Done():
			return
		}
	}
}
