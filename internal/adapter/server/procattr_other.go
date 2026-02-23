//go:build !linux

package server

import "syscall"

// sysProcAttr returns process attributes that put the child in its own process
// group. Pdeathsig is not available on non-Linux platforms.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}
