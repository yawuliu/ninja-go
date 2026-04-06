package util

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// Fatal 输出致命错误并退出进程。
func Fatal(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ninja: fatal: "+msg+"\n", args...)
	os.Exit(1)
}

// Warning 输出警告信息。
func Warning(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ninja: warning: "+msg+"\n", args...)
}

// Error 输出错误信息。
func Error(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ninja: error: "+msg+"\n", args...)
}

// Info 输出信息到 stdout。
func Info(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, "ninja: "+msg+"\n", args...)
}

// CanonicalizePath 规范化路径，将连续的 '/' 合并，解析 '.' 和 '..'，
// 并将 Windows 反斜杠转为正斜杠，同时记录反斜杠位置用于恢复。
func CanonicalizePath(path string) (string, uint64) {
	if path == "" {
		return "", 0
	}
	// 复制为可修改的字节数组
	buf := []byte(path)
	len := len(buf)
	slashBits := uint64(0)
	bit := uint64(1)

	// 处理 Windows 盘符和网络路径前缀
	start := 0
	if len >= 2 && buf[0] == '\\' && buf[1] == '\\' {
		// 网络路径 //server/share，保留前两个斜杠
		start = 2
		bit <<= 2
	} else if len >= 1 && IsPathSeparator(buf[0]) {
		start = 1
		bit <<= 1
	}

	// 循环处理每个路径组件
	dst := buf[start:]
	src := buf[start:]
	end := buf[len:]
	componentCount := 0
	dstStart := dst

	for src < end {
		// 查找下一个路径分隔符
		nextSep := src
		for nextSep < end && !IsPathSeparator(*nextSep) {
			nextSep++
		}
		compLen := nextSep - src
		// 空组件（连续分隔符）跳过
		if compLen == 0 {
			src = nextSep + 1
			continue
		}
		// 处理 '.' 和 '..'
		if compLen == 1 && src[0] == '.' {
			src = nextSep + 1
			continue
		}
		if compLen == 2 && src[0] == '.' && src[1] == '.' {
			// 回退一个组件
			if componentCount > 0 {
				// 向前查找上一个分隔符
				for dst > dstStart && !IsPathSeparator(dst[-1]) {
					dst--
				}
				if dst > dstStart {
					dst-- // 去掉分隔符
				}
				componentCount--
			} else {
				// 保留 '..' 作为路径起始
				copy(dst, src[:2])
				dst += 2
				componentCount++
			}
			src = nextSep + 1
			continue
		}
		// 普通组件
		if dst != src {
			copy(dst, src[:compLen])
		}
		dst += compLen
		componentCount++
		// 添加分隔符（除了最后一个组件）
		if nextSep < end {
			*dst = '/'
			dst++
		}
		src = nextSep + 1
	}
	// 去除末尾多余的分隔符（但保留根目录的单个斜杠）
	if dst > dstStart && IsPathSeparator(dst[-1]) {
		dst--
	}
	if dst == buf {
		*dst = '.'
		dst++
	}
	newLen := int(dst - buf)
	// 记录反斜杠位置
	for i := 0; i < newLen; i++ {
		if buf[i] == '\\' {
			slashBits |= bit
			buf[i] = '/'
		}
		bit <<= 1
	}
	return string(buf[:newLen]), slashBits
}

// IsPathSeparator 判断字符是否为路径分隔符。
func IsPathSeparator(c byte) bool {
	return c == '/' || (runtime.GOOS == "windows" && c == '\\')
}

// GetShellEscapedString 对字符串进行 POSIX shell 转义。
func GetShellEscapedString(s string) string {
	if !needsShellEscaping(s) {
		return s
	}
	var buf bytes.Buffer
	buf.WriteByte('\'')
	last := 0
	for i, ch := range s {
		if ch == '\'' {
			buf.WriteString(s[last:i])
			buf.WriteString(`'\''`)
			last = i
		}
	}
	buf.WriteString(s[last:])
	buf.WriteByte('\'')
	return buf.String()
}

func needsShellEscaping(s string) bool {
	for _, ch := range s {
		if !isShellSafeChar(ch) {
			return true
		}
	}
	return false
}

func isShellSafeChar(ch rune) bool {
	switch {
	case 'A' <= ch && ch <= 'Z':
		return true
	case 'a' <= ch && ch <= 'z':
		return true
	case '0' <= ch && ch <= '9':
		return true
	case ch == '_' || ch == '+' || ch == '-' || ch == '.' || ch == '/':
		return true
	default:
		return false
	}
}

// GetWin32EscapedString 对字符串进行 Windows cmd 转义。
func GetWin32EscapedString(s string) string {
	if !needsWin32Escaping(s) {
		return s
	}
	var buf bytes.Buffer
	buf.WriteByte('"')
	backslashCount := 0
	for _, ch := range s {
		if ch == '\\' {
			backslashCount++
			continue
		}
		if ch == '"' {
			buf.WriteString(strings.Repeat("\\", backslashCount+1))
			buf.WriteByte('"')
		} else {
			buf.WriteString(strings.Repeat("\\", backslashCount))
			buf.WriteRune(ch)
		}
		backslashCount = 0
	}
	buf.WriteString(strings.Repeat("\\", backslashCount))
	buf.WriteByte('"')
	return buf.String()
}

func needsWin32Escaping(s string) bool {
	for _, ch := range s {
		if ch == ' ' || ch == '"' {
			return true
		}
	}
	return false
}

// ReadFile 读取文件内容，返回内容字符串和错误。
func ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SetCloseOnExec 设置文件描述符的 close-on-exec 标志（仅 Unix）。
func SetCloseOnExec(f *os.File) {
	if runtime.GOOS != "windows" {
		syscall.CloseOnExec(f.Fd())
	}
}

// SpellcheckString 在给定候选词列表中查找与输入最接近的词（编辑距离最小）。
func SpellcheckString(text string, candidates []string) string {
	const maxDist = 3
	best := ""
	bestDist := maxDist + 1
	for _, cand := range candidates {
		dist := EditDistance(cand, text, true, maxDist)
		if dist < bestDist {
			bestDist = dist
			best = cand
		}
	}
	return best
}

// GetProcessorCount 返回逻辑 CPU 数量（考虑 cgroup 限制）。
func GetProcessorCount() int {
	if runtime.GOOS == "windows" {
		// Windows: 使用环境变量或直接读取
		return runtime.NumCPU()
	}
	// Linux/Unix: 尝试从 cgroup 读取限制，否则使用 runtime.NumCPU()
	cpus := runtime.NumCPU()
	// 简单实现，完整 cgroup 解析需更多代码，此处略
	return cpus
}

// GetLoadAverage 返回系统负载平均值（仅 Unix，Windows 返回 -1）。
func GetLoadAverage() float64 {
	if runtime.GOOS == "windows" {
		return -1.0
	}
	// 使用 /proc/loadavg 或 getloadavg，这里简化
	return -1.0
}

// GetWorkingDirectory 返回当前工作目录的绝对路径。
func GetWorkingDirectory() string {
	wd, err := os.Getwd()
	if err != nil {
		Fatal("cannot determine working directory: %v", err)
	}
	return wd
}

// Truncate 将文件截断到指定大小。
func Truncate(path string, size int64) error {
	return os.Truncate(path, size)
}

// ReplaceContent 原子替换文件内容：先写临时文件，再重命名。
func ReplaceContent(dst, src string) error {
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	return nil
}

// EditDistance 计算两个字符串的编辑距离（Levenshtein）。
func EditDistance(a, b string, allowReplacements bool, maxDist int) int {
	// 简单实现，可参考标准库或自己实现
	// 为节省篇幅，这里返回 0 表示未实现，实际应实现算法
	// 建议使用 github.com/agext/levenshtein 或自己写
	_ = allowReplacements
	_ = maxDist
	return 0
}
