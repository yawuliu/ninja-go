package main

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"unsafe"
)

// LineType 表示输出行的类型
type LineType int

const (
	FULL LineType = iota // 需要省略过长的行
	ELIDE
)

// LinePrinter 负责智能终端输出（覆盖行、颜色等）
type LinePrinter struct {
	have_blank_line_       bool
	console_locked_        bool
	line_buffer_           string
	lineType               LineType
	output_buffer_         bytes.Buffer
	smart_terminal_        bool
	supports_color_        bool
	console_               winHandle // Windows 专用
	procWriteConsoleOutput *winLazyProc
}

// NewLinePrinter 创建并初始化 LinePrinter
func NewLinePrinter() *LinePrinter {
	lp := &LinePrinter{
		have_blank_line_: true,
		console_locked_:  false,
		smart_terminal_:  false,
		supports_color_:  false,
		console_:         winInvalidHandle,
	}

	term := os.Getenv("TERM")
	if runtime.GOOS == "windows" {
		if term == "dumb" {
			lp.smart_terminal_ = false
		} else {
			lp.console_ = winHandle(os.Stdout.Fd())
			var csbi winConsoleScreenBufferInfo
			err := winGetConsoleScreenBufferInfo(lp.console_, &csbi)
			lp.smart_terminal_ = (err == nil)
		}
	} else {
		// Unix-like: 检查 stdout 是否为终端且 TERM != "dumb"
		//if isTerminal(int(os.Stdout.Fd())) && term != "dumb" {
		//	lp.smart_terminal_ = true
		//}
	}
	lp.supports_color_ = lp.smart_terminal_

	if runtime.GOOS == "windows" && lp.supports_color_ {
		var mode uint32
		if winGetConsoleMode(lp.console_, &mode) == nil {
			// 尝试启用 ANSI 转义序列支持
			if err := winSetConsoleMode(lp.console_, mode|winEnableVirtualTerminal); err != nil {
				lp.supports_color_ = false
			}
		}
	}

	if !lp.supports_color_ {
		clicolorForce := os.Getenv("CLICOLOR_FORCE")
		if clicolorForce != "" && clicolorForce != "0" {
			lp.supports_color_ = true
		}
	}
	lp.procWriteConsoleOutput = winNewLazyProc("WriteConsoleOutputW")

	return lp
}

// CHAR_INFO 的 Windows 映射
type charInfo struct {
	unicodeChar uint16 // wchar
	attributes  uint16 // word
}

func (lp *LinePrinter) WriteConsoleOutput(console winHandle, buffer []charInfo, bufferSize, bufferCoord winCoord, writeRegion *winSmallRect) {
	ret, _, err := lp.procWriteConsoleOutput.Call(
		uintptr(console),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(*(*int32)(unsafe.Pointer(&bufferSize))),
		uintptr(*(*int32)(unsafe.Pointer(&bufferCoord))),
		uintptr(unsafe.Pointer(writeRegion)),
	)
	if ret == 0 {
		fmt.Println("WriteConsoleOutput failed:", err)
	} else {
		fmt.Println("Successfully wrote to console.")
	}
}

// Print 输出一行，根据 smart_terminal 和 type 决定是否覆盖前一行
func (lp *LinePrinter) Print(toPrint string, typ LineType) {
	if lp.console_locked_ {
		lp.line_buffer_ = toPrint
		lp.lineType = typ
		return
	}

	if lp.smart_terminal_ {
		fmt.Print("\r") // 覆盖前一行
	}

	if lp.smart_terminal_ && typ == ELIDE {
		if runtime.GOOS == "windows" {
			var csbi winConsoleScreenBufferInfo
			if err := winGetConsoleScreenBufferInfo(lp.console_, &csbi); err == nil {
				width := int(csbi.Size.X)
				toPrint = elideMiddle(toPrint, width)
				if lp.supports_color_ {
					fmt.Printf("%s\x1B[K", toPrint) // 清除到行尾
					os.Stdout.Sync()
				} else {
					// 使用 WriteConsoleOutput 更新缓冲区但不移动光标
					bufSize := winCoord{X: csbi.Size.X, Y: 1}
					zeroZero := winCoord{X: 0, Y: 0}
					target := winSmallRect{
						Left:   csbi.CursorPosition.X,
						Top:    csbi.CursorPosition.Y,
						Right:  csbi.CursorPosition.X + csbi.Size.X - 1,
						Bottom: csbi.CursorPosition.Y,
					}
					charData := make([]charInfo, csbi.Size.X)
					for i := 0; i < int(csbi.Size.X); i++ {
						if i < len(toPrint) {
							charData[i].unicodeChar = uint16(toPrint[i])
						} else {
							charData[i].unicodeChar = uint16(' ')
						}
						charData[i].attributes = csbi.Attributes
					}
					lp.WriteConsoleOutput(lp.console_, charData, bufSize, zeroZero, &target)
				}
			}
		} else {
			// Unix-like: 获取终端宽度
			//width := getTerminalWidth()
			//if width > 0 {
			//	toPrint = elideMiddle(toPrint, width)
			//}
			//fmt.Printf("%s\x1B[K", toPrint)
			os.Stdout.Sync()
		}
		lp.have_blank_line_ = false
	} else {
		fmt.Printf("%s\n", toPrint)
		os.Stdout.Sync()
	}
}

// PrintOrBuffer 直接输出数据（可能包含 null 字节），若锁定时则缓冲
func (lp *LinePrinter) PrintOrBuffer(data []byte) {
	if lp.console_locked_ {
		lp.output_buffer_.Write(data)
	} else {
		os.Stdout.Write(data)
	}
}

// PrintOnNewLine 确保在新行上输出（必要时先换行）
func (lp *LinePrinter) PrintOnNewLine(toPrint string) {
	if lp.console_locked_ && lp.line_buffer_ != "" {
		lp.output_buffer_.WriteString(lp.line_buffer_)
		lp.output_buffer_.WriteByte('\n')
		lp.line_buffer_ = ""
	}
	if !lp.have_blank_line_ {
		lp.PrintOrBuffer([]byte("\n"))
	}
	if toPrint != "" {
		lp.PrintOrBuffer([]byte(toPrint))
	}
	lp.have_blank_line_ = (toPrint == "") || (toPrint[len(toPrint)-1] == '\n')
}

// SetConsoleLocked 锁定/解锁控制台输出（用于多线程保护）
func (lp *LinePrinter) SetConsoleLocked(locked bool) {
	if locked == lp.console_locked_ {
		return
	}
	if locked {
		lp.PrintOnNewLine("")
	}
	lp.console_locked_ = locked
	if !locked {
		lp.PrintOnNewLine(lp.output_buffer_.String())
		if lp.line_buffer_ != "" {
			lp.Print(lp.line_buffer_, lp.lineType)
		}
		lp.output_buffer_.Reset()
		lp.line_buffer_ = ""
	}
}

// --- 辅助函数（假设存在，这里仅占位）---
func elideMiddle(s string, width int) string {
	// 原始 C++ 函数，此处留空，实际实现需要截断中间部分
	if len(s) <= width {
		return s
	}
	// 简单截断
	return s[:width-3] + "..."
}

// isTerminal 检查文件描述符是否为终端（Unix）
//func isTerminal(fd int) bool {
//	var termios syscall.Termios
//	_, _, err := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
//	return err == 0
//}

// getTerminalWidth 获取终端宽度（Unix）
//func getTerminalWidth() int {
//	var ws struct {
//		row, col uint16
//		xp, yp   uint16
//	}
//	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stdout), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
//	if err != 0 {
//		return 0
//	}
//	return int(ws.col)
//}

func (lp *LinePrinter) is_smart_terminal() bool       { return lp.smart_terminal_ }
func (lp *LinePrinter) set_smart_terminal(smart bool) { lp.smart_terminal_ = smart }

func (lp *LinePrinter) supports_color() bool { return lp.supports_color_ }
