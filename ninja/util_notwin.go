//go:build !windows

package main

// getWindowsACP returns 0 on non-Windows platforms (ToolWinCodePage is not available).
func getWindowsACP() uint32 {
	return 0
}
