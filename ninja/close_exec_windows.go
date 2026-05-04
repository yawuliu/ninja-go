//go:build windows

package main

import "syscall"

func setCloseOnExec(fd uintptr) {
	syscall.CloseOnExec(syscall.Handle(fd))
}
