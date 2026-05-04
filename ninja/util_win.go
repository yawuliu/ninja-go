//go:build windows

package main

import (
	"syscall"
)

var (
	kernel32   = syscall.NewLazyDLL("kernel32.dll")
	procGetACP = kernel32.NewProc("GetACP")
)

// getWindowsACP returns the current Windows ANSI code page (e.g., 65001 for UTF-8).
func getWindowsACP() uint32 {
	ret, _, _ := procGetACP.Call()
	return uint32(ret)
}
