//go:build linux

package main

import "syscall"

func workerProcessAttributes() *syscall.SysProcAttr {
	// If init kills the supervisor, ask its worker to close audio and helpers.
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}
