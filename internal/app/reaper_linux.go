package app

import (
	"os"
	"os/signal"
	"syscall"
)

// startReaper starts a background goroutine that reaps zombie child processes.
// When codetap is PID 1 inside a container, orphaned grandchildren (spawned by
// code-server extensions) get reparented to it. Without an explicit reaper they
// accumulate as zombies because codetap only calls cmd.Wait on its direct child.
func startReaper() {
	go func() {
		ch := make(chan os.Signal, 8)
		signal.Notify(ch, syscall.SIGCHLD)
		for range ch {
			for {
				var ws syscall.WaitStatus
				pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
				if pid <= 0 || err != nil {
					break
				}
			}
		}
	}()
}
