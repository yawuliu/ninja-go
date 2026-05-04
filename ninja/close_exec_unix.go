//go:build !windows

package main

import "syscall"

func setCloseOnExec(fd uintptr) {
	syscall.CloseOnExec(int(fd))
}
