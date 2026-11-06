//go:build !windows

package main

// noAutostart — приложение автозапуск не ведёт (macOS: LaunchAgent ставит .pkg).
type noAutostart struct{}

func newAutostart() autostarter { return noAutostart{} }

func (noAutostart) autostartSupported() bool        { return false }
func (noAutostart) autostartEnabled() (bool, error) { return false, nil }
func (noAutostart) setAutostart(bool) error         { return nil }
