//go:build !linux

package main

import "syscall"

func workerProcessAttributes() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }
