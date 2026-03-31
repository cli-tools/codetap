package server

import "syscall"

// sysProcAttr returns process attributes that put the child in its own session.
// Setsid detaches code-server from the controlling terminal so that child
// processes (e.g. git invoked by VS Code) cannot prompt on /dev/tty.
// Pdeathsig is a Linux-only safety net: if codetap dies unexpectedly,
// the kernel sends SIGTERM to the direct child.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid:    true,
		Pdeathsig: syscall.SIGTERM,
	}
}
