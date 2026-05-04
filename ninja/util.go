package main

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"unicode"
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
		setCloseOnExec(f.Fd())
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

// GetLoadAverage 返回系统负载平均值（仅 Unix，Windows 返回 -1）。
func GetLoadAverage() float64 {
	if runtime.GOOS == "windows" {
		return -1.0
	}
	// 使用 /proc/loadavg 或 getloadavg，这里简化
	return -1.0
}

// Truncate 将文件截断到指定大小。
func Truncate(path string, size int64, err *string) bool {
	if truncErr := os.Truncate(path, size); truncErr != nil {
		*err = truncErr.Error()
		return false
	}
	return true
}

// ReplaceContent 原子替换文件内容：先写临时文件，再重命名。
func ReplaceContent(dst, src string, err *string) bool {
	// On Windows, os.Rename cannot replace an existing file, so remove it first.
	if runtime.GOOS == "windows" {
		os.Remove(dst)
	}
	if rename_err := os.Rename(src, dst); rename_err != nil {
		*err = rename_err.Error()
		return false
	}
	return true
}

// DirName 返回路径的目录名，与 C++ 版本行为一致：
// 找到最后一个路径分隔符，然后向前跳过连续的分隔符，返回之前的子串。
// 例如：DirName("foo/bar/") -> "foo"
//
//	DirName("foo//bar") -> "foo"
//	DirName("foo") -> ""
func DirName(path string) string {
	// 根据操作系统确定分隔符集合
	var separators string
	if os.PathSeparator == '/' {
		separators = "/"
	} else {
		separators = "\\/"
	}
	// 查找最后一个分隔符
	lastSep := strings.LastIndexAny(path, separators)
	if lastSep == -1 {
		return ""
	}
	// 向前跳过连续的分隔符
	for lastSep > 0 && strings.ContainsAny(string(path[lastSep-1]), separators) {
		lastSep--
	}
	return path[:lastSep]
}

// PathDecanonicalized 根据 slash_bits 位掩码将路径中的正斜杠恢复为反斜杠（仅 Windows）。
// 在非 Windows 平台上，直接返回原路径。
func PathDecanonicalized(path string, slashBits uint64) string {
	if runtime.GOOS != "windows" {
		return path
	}
	// 复制为可修改的字节切片
	buf := []byte(path)
	mask := uint64(1)
	for i := 0; i < len(buf); i++ {
		if buf[i] == '/' {
			if slashBits&mask != 0 {
				buf[i] = '\\'
			}
			mask <<= 1
		}
	}
	return string(buf)
}

// CanonicalizePathString converts a path string to canonical form.
// It modifies the string in place by converting to a byte slice,
// calling CanonicalizePathBytes, and updating the string.
func CanonicalizePathString(path *string, slashBits *uint64) {
	if path == nil {
		return
	}
	b := []byte(*path)
	length := len(b)
	if length > 0 {
		CanonicalizePathBytes(b, &length, slashBits)
		*path = string(b[:length])
	} else {
		*path = ""
	}
}

// CanonicalizePathBytes canonicalizes a path in-place.
// path: a mutable byte slice representing the path.
// lenPtr: pointer to the length of the path (updated on return).
// slashBits: (Windows) bitmask where each set bit indicates a backslash position.
func CanonicalizePathBytes(path []byte, lenPtr *int, slashBits *uint64) {
	if lenPtr == nil || *lenPtr == 0 {
		return
	}
	length := *lenPtr
	start := 0
	dst := start
	dstStart := dst
	src := start
	end := length

	isPathSeparator := func(c byte) bool {
		if runtime.GOOS == "windows" {
			return c == '/' || c == '\\'
		}
		return c == '/'
	}

	// For absolute paths, skip the leading directory separator
	// as this one should never be removed_ from the result.
	if isPathSeparator(path[src]) {
		if runtime.GOOS == "windows" {
			// Windows network path starts with //
			if src+2 <= end && isPathSeparator(path[src+1]) {
				src += 2
				dst += 2
			} else {
				src++
				dst++
			}
		} else {
			src++
			dst++
		}
		dstStart = dst
	} else {
		// For relative paths, skip any leading ../ as these are quite common
		for src+3 <= end && path[src] == '.' && path[src+1] == '.' && isPathSeparator(path[src+2]) {
			src += 3
			dst += 3
		}
	}

	// Loop over all components except the last one
	componentCount := 0
	dst0 := dst
	srcNext := src
	for src < end {
		var nextSep int
		if runtime.GOOS != "windows" {
			// Use bytes.IndexByte for faster lookup
			sub := path[src:end]
			idx := bytes.IndexByte(sub, '/')
			if idx == -1 {
				break // last component
			}
			nextSep = src + idx
		} else {
			// Windows: scan_ for '/' or '\\'
			nextSep = src
			for nextSep < end && !isPathSeparator(path[nextSep]) {
				nextSep++
			}
			if nextSep == end {
				break
			}
		}
		srcNext = nextSep + 1
		componentLen := nextSep - src

		if componentLen <= 2 {
			if componentLen == 0 {
				// Ignore empty component
				src = srcNext
				continue
			}
			if path[src] == '.' {
				if componentLen == 1 {
					// Ignore '.'
					src = srcNext
					continue
				} else if componentLen == 2 && path[src+1] == '.' {
					// Process '..'
					if componentCount > 0 {
						// Move back to previous component
						componentCount--
						for dst > dst0 && !isPathSeparator(path[dst-1]) {
							dst--
						}
						// Also move back the separator
						if dst > dst0 && isPathSeparator(path[dst-1]) {
							dst--
						}
					} else {
						// Keep the '..'
						if dst != src {
							copy(path[dst:dst+3], path[src:src+3])
						}
						dst += 3
					}
					src = srcNext
					continue
				}
			}
		}
		componentCount++

		// Copy component including trailing separator
		copyLen := srcNext - src
		if dst != src {
			copy(path[dst:dst+copyLen], path[src:srcNext])
		}
		dst += copyLen
		src = srcNext
	}

	// Handle the last component (no trailing separator)
	componentLen := end - src
	if componentLen > 0 {
		if !(componentLen == 1 && path[src] == '.') && // ignore trailing '.'
			!(componentLen == 2 && path[src] == '.' && path[src+1] == '.') {
			// Normal component: copy
			if dst != src {
				copy(path[dst:dst+componentLen], path[src:src+componentLen])
			}
			dst += componentLen
		} else if componentLen == 2 && path[src] == '.' && path[src+1] == '.' {
			// Handle trailing '..'
			if componentCount > 0 {
				// Back up
				for dst > dst0 && !isPathSeparator(path[dst-1]) {
					dst--
				}
				if dst > dst0 && isPathSeparator(path[dst-1]) {
					dst--
				}
			} else {
				// Keep '..'
				if dst != src {
					copy(path[dst:dst+2], path[src:src+2])
				}
				dst += 2
			}
		}
	}

	// Remove trailing path separator if any
	if dst > dstStart && isPathSeparator(path[dst-1]) {
		dst--
	}

	if dst == start {
		path[dst] = '.'
		dst++
	}

	*lenPtr = dst - start

	// On Windows, record backslash positions and convert them to '/'
	if runtime.GOOS == "windows" {
		var bits uint64 = 0
		var bitsMask uint64 = 1
		for i := start; i < start+*lenPtr; i++ {
			switch path[i] {
			case '\\':
				bits |= bitsMask
				path[i] = '/'
				fallthrough
			case '/':
				bitsMask <<= 1
			}
		}
		if slashBits != nil {
			*slashBits = bits
		}
	} else if slashBits != nil {
		*slashBits = 0
	}
}

func StripAnsiEscapeCodes(in string) string {
	var stripped strings.Builder
	stripped.Grow(len(in))

	for i := 0; i < len(in); i++ {
		if in[i] != '\x1b' {
			// Not an escape code.
			stripped.WriteByte(in[i])
			continue
		}

		// Only strip CSIs for now.
		if i+1 >= len(in) {
			break
		}
		if in[i+1] != '[' {
			continue // Not a CSI.
		}
		i += 2

		// Skip everything up to and including the next [a-zA-Z].
		for i < len(in) && !unicode.IsLetter(rune(in[i])) {
			i++
		}
	}
	return stripped.String()
}
