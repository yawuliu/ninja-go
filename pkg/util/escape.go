package util

import (
	"strings"
	"unicode"
)

// splitEscapedFields 按空白字符分割字符串，但忽略反斜杠转义的空格。
// 同时保留反斜杠，后续由 unescapeDepfile 统一处理转义。
func SplitEscapedFields(s string) []string {
	var result []string
	var token strings.Builder
	escaped := false
	for _, ch := range s {
		if escaped {
			token.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			token.WriteRune(ch)
			escaped = true
			continue
		}
		if unicode.IsSpace(ch) {
			if token.Len() > 0 {
				result = append(result, token.String())
				token.Reset()
			}
			continue
		}
		token.WriteRune(ch)
	}
	if token.Len() > 0 {
		result = append(result, token.String())
	}
	return result
}

// findUnescapedColon 返回字符串中第一个未被反斜杠转义的冒号的位置，并跳过盘符（如 "C:"）
func FindUnescapedColon(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++ // 跳过转义字符
			continue
		}
		if s[i] == ':' {
			// 检查是否是盘符（如 "C:"），盘符后面跟着 '\\' 或 '/' 或行尾？简单判断：如果冒号前是一个字母且冒号是第二个字符，则跳过
			//if i == 1 && ('a' <= s[0] && s[0] <= 'z' || 'A' <= s[0] && s[0] <= 'Z') {
			//	continue
			//}
			return i
		}
	}
	return -1
}

// unescapeDepfile 处理 depfile 中的转义序列，将 "\ " 转换为 " "，将 "\\" 转换为 "\"，将 "\#" 转换为 "#"。
func UnescapeDepfile(s string) string {
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case ' ':
				result.WriteByte(' ')
				i++
			case '#':
				result.WriteByte('#')
				i++
			case ':':
				result.WriteByte(':')
				i++
			case '\\':
				result.WriteByte('\\')
				i++
			default:
				// 保留原转义（如 \:）
				result.WriteByte('\\')
				result.WriteByte(next)
				i++
			}
		} else {
			result.WriteByte(s[i])
		}
	}
	return result.String()
}
