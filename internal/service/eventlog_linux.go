//go:build linux

package service

import "io"

// Unter systemd genuegt stderr: der Dienst laeuft mit journald als Ausgabeziel,
// und journalctl -u itop-agent zeigt alles. Ein zusaetzlicher Protokollpfad
// waere nur eine zweite Stelle, an der man suchen muesste.
func LogWriter(fallback io.Writer) (io.Writer, func()) {
	return fallback, func() {}
}

func registerEventSource() error { return nil }
func removeEventSource() error   { return nil }
