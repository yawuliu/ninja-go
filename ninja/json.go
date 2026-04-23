package main

import (
	"os"
)

// EncodeJSONString 将字符串转义为 JSON 字符串格式（不包含外层双引号）。
func EncodeJSONString(s string) string {
	const hexDigits = "0123456789abcdef"
	// 预估容量
	out := make([]byte, 0, len(s)*6/5)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\b':
			out = append(out, '\\', 'b')
		case '\f':
			out = append(out, '\\', 'f')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		case '\\':
			out = append(out, '\\', '\\')
		case '"':
			out = append(out, '\\', '"')
		default:
			if c < 0x20 {
				// 控制字符转义为 \u00xx
				out = append(out, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
			} else {
				out = append(out, c)
			}
		}
	}
	return string(out)
}

// PrintJSONString 输出 JSON 转义后的字符串（不包含外层引号）。
func PrintJSONString(s string) {
	escaped := EncodeJSONString(s)
	os.Stdout.WriteString(escaped)
}
