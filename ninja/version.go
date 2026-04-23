package main

import (
	"strconv"
	"strings"
)

// ParseVersion 解析版本字符串，格式如 "1", "1.0", "1.0-extra"。
// 返回 major 和 minor 版本号。忽略后缀（如 "-extra"）。
func ParseVersion(version string) (major, minor int) {
	// 查找第一个 '.' 位置
	dotIdx := strings.Index(version, ".")
	if dotIdx == -1 {
		// 没有 '.'，尝试解析为整数（可能包含后缀如 "1-extra"）
		// 提取数字前缀
		numStr := version
		for i, ch := range numStr {
			if ch < '0' || ch > '9' {
				numStr = numStr[:i]
				break
			}
		}
		major, _ = strconv.Atoi(numStr)
		minor = 0
		return
	}
	// 解析主版本号（点之前的部分）
	majorStr := version[:dotIdx]
	major, _ = strconv.Atoi(majorStr)
	// 解析次版本号（点之后到下一个点或非数字）
	rest := version[dotIdx+1:]
	// 提取数字前缀（忽略后缀）
	minorStr := rest
	for i, ch := range minorStr {
		if ch < '0' || ch > '9' {
			minorStr = minorStr[:i]
			break
		}
	}
	minor, _ = strconv.Atoi(minorStr)
	return
}
