//go:build !linux

package app

// startReaper is a no-op on non-Linux platforms. The zombie reaper is only
// needed when codetap runs as PID 1 inside a Linux container.
func startReaper() {}
