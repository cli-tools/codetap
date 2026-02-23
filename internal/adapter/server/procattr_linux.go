package server

import "syscall"

// sysProcAttr returns process attributes that put the child in its own process
// group. Pdeathsig is a Linux-only safety net: if codetap dies unexpectedly,
// the kernel sends SIGTERM to the direct child.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGTERM,
	}
}
