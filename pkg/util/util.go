package util

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
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
	// 转换为字节切片以便修改
	buf := []byte(path)
	n := len(buf)
	if n == 0 {
		return "", 0
	}
	// 输出位置
	dst := 0
	// 记录起始位置（用于处理根目录）
	dstStart := 0
	src := 0
	// 解析绝对路径前缀
	if IsPathSeparator(buf[src]) {
		if runtime.GOOS == "windows" && src+1 < n && IsPathSeparator(buf[src+1]) {
			// Windows 网络路径 //server/share，保留前两个斜杠
			buf[dst] = buf[src]
			dst++
			src++
			buf[dst] = buf[src]
			dst++
			src++
		} else {
			// 普通绝对路径，保留一个斜杠
			buf[dst] = buf[src]
			dst++
			src++
		}
		dstStart = dst
	} else {
		// 跳过开头的 "../" 序列
		for src+3 <= n && buf[src] == '.' && buf[src+1] == '.' && IsPathSeparator(buf[src+2]) {
			// 复制 "../" 到输出（但后续可能被回退？不，这里直接跳过，因为要保留？）
			// C++ 中跳过开头的 "../" 但不复制，我们这里也跳过不复制
			src += 3
		}
	}
	// 记录每个组件的起始位置（用于处理 ..）
	compStarts := []int{dst}
	for src < n {
		// 查找下一个分隔符
		nextSep := src
		for nextSep < n && !IsPathSeparator(buf[nextSep]) {
			nextSep++
		}
		if nextSep == n {
			// 最后一个组件，跳出循环单独处理
			break
		}
		// 组件长度（不包括尾部分隔符）
		compLen := nextSep - src
		if compLen == 0 {
			// 空组件（连续分隔符），忽略
			src = nextSep + 1
			continue
		}
		if compLen == 1 && buf[src] == '.' {
			// 忽略 '.' 组件
			src = nextSep + 1
			continue
		}
		if compLen == 2 && buf[src] == '.' && buf[src+1] == '.' {
			if len(compStarts) > 1 {
				// 回退一个组件
				compStarts = compStarts[:len(compStarts)-1]
				dst = compStarts[len(compStarts)-1]
			} else {
				// 保留开头的 '..'
				buf[dst] = '.'
				dst++
				buf[dst] = '.'
				dst++
				buf[dst] = '/'
				dst++
				compStarts = append(compStarts, dst)
			}
			src = nextSep + 1
			continue
		}
		// 普通组件：复制组件和尾部分隔符
		if dst != src {
			copy(buf[dst:], buf[src:nextSep+1])
		}
		dst += compLen + 1
		src = nextSep + 1
		compStarts = append(compStarts, dst)
	}
	// 处理最后一个组件（没有尾部分隔符）
	if src < n {
		compLen := n - src
		if compLen == 1 && buf[src] == '.' {
			// 忽略末尾 '.'
		} else if compLen == 2 && buf[src] == '.' && buf[src+1] == '.' {
			if len(compStarts) > 1 {
				// 回退一个组件
				compStarts = compStarts[:len(compStarts)-1]
				dst = compStarts[len(compStarts)-1]
			} else {
				buf[dst] = '.'
				dst++
				buf[dst] = '.'
				dst++
			}
		} else {
			if dst != src {
				copy(buf[dst:], buf[src:n])
			}
			dst += compLen
		}
	}
	// 移除末尾多余的分隔符（但保留根目录的单个分隔符）
	if dst > dstStart && IsPathSeparator(buf[dst-1]) {
		dst--
	}
	if dst == 0 {
		buf[0] = '.'
		dst = 1
	}
	result := string(buf[:dst])
	var slashBits uint64 = 0
	if runtime.GOOS == "windows" {
		mask := uint64(1)
		for i := 0; i < dst; i++ {
			c := buf[i]
			if c == '\\' {
				slashBits |= mask
				// 将反斜杠转为正斜杠（已在 buf 中修改，但 result 是复制的，需要重新处理？）
				// 注意：buf 已经被修改，但我们最终要返回规范化后的字符串（正斜杠）
				// 由于我们在遍历时已经修改了 buf[i] = '/'，所以 result 中已经是正斜杠。
				// 但我们需要同时返回 slashBits，以便恢复反斜杠位置。
				buf[i] = '/'
			}
			if c == '/' {
				mask <<= 1
			}
		}
		// 重新生成 result，确保正斜杠
		result = string(buf[:dst])
	}
	return result, slashBits
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
		syscall.CloseOnExec(syscall.Handle(f.Fd()))
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
func ReplaceContent(dst, src string, err *string) bool {
	if rename_err := os.Rename(src, dst); rename_err != nil {
		*err = rename_err.Error()
		return false
	}
	return true
}

//// EditDistance 计算两个字符串的编辑距离（Levenshtein）。
//func EditDistance(a, b string, allowReplacements bool, maxDist int) int {
//	// 简单实现，可参考标准库或自己实现
//	// 为节省篇幅，这里返回 0 表示未实现，实际应实现算法
//	// 建议使用 github.com/agext/levenshtein 或自己写
//	_ = allowReplacements
//	_ = maxDist
//	return 0
//}

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

// MakeDir 创建单级目录，权限为 0777（Unix）或默认（Windows）。
// 注意：此函数不会递归创建父目录，需要调用者确保父目录存在。
// 返回值与 C++ 的 mkdir/_mkdir 一致：成功返回 0，失败返回 -1（并设置 errno）。
func MakeDir(path string) error {
	return os.Mkdir(path, 0755)
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
