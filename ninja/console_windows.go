//go:build windows

package main

import "golang.org/x/sys/windows"

type winHandle = windows.Handle
type winLazyProc = windows.LazyProc
type winConsoleScreenBufferInfo = windows.ConsoleScreenBufferInfo
type winCoord = windows.Coord
type winSmallRect = windows.SmallRect

const winInvalidHandle = windows.InvalidHandle
const winEnableVirtualTerminal = windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING

var winGetConsoleScreenBufferInfo = windows.GetConsoleScreenBufferInfo
var winGetConsoleMode = windows.GetConsoleMode
var winSetConsoleMode = windows.SetConsoleMode

func winNewLazyProc(name string) *windows.LazyProc {
	return windows.NewLazySystemDLL("kernel32.dll").NewProc(name)
}
