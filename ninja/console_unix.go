//go:build !windows

package main

type winHandle = uintptr
type winLazyProc struct{}

type winCoord struct{ X, Y int16 }
type winSmallRect struct{ Left, Top, Right, Bottom int16 }
type winConsoleScreenBufferInfo struct {
	Size           winCoord
	CursorPosition winCoord
	Attributes     uint16
}

const winInvalidHandle = ^winHandle(0)
const winEnableVirtualTerminal = 0x0004

func winGetConsoleScreenBufferInfo(h winHandle, info *winConsoleScreenBufferInfo) error { return nil }
func winGetConsoleMode(h winHandle, mode *uint32) error                                 { return nil }
func winSetConsoleMode(h winHandle, mode uint32) error                                  { return nil }
func winNewLazyProc(name string) *winLazyProc                                           { return &winLazyProc{} }
func (p *winLazyProc) Call(a ...uintptr) (uintptr, uintptr, error)                      { return 0, 0, nil }
