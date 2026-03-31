//go:build !linux

package server

import "syscall"

// sysProcAttr returns process attributes that put the child in its own session.
// Setsid detaches code-server from the controlling terminal so that child
// processes (e.g. git invoked by VS Code) cannot prompt on /dev/tty.
// Pdeathsig is not available on non-Linux platforms.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true,
	}
}
