package builder

import (
	"strings"
)

// DepfileParserOptions 目前为空，保留用于扩展
type DepfileParserOptions struct{}

// DepfileParser 解析 depfile 内容
type DepfileParser struct {
	Outs    []string
	Ins     []string
	options DepfileParserOptions
}

// NewDepfileParser 创建解析器实例
func NewDepfileParser(options DepfileParserOptions) *DepfileParser {
	return &DepfileParser{
		options: options,
	}
}

// Parse 解析 depfile 内容，返回错误（如果有）
func (p *DepfileParser) Parse(content string) error {
	p.Outs = nil
	p.Ins = nil
	in := []rune(content)
	pos := 0
	n := len(in)
	parsingTargets := true
	haveTarget := false
	poisonedInput := false
	isFirstLine := true

	for pos < n {
		// 跳过前导空格
		for pos < n && (in[pos] == ' ' || in[pos] == '\t') {
			pos++
		}
		if pos >= n {
			break
		}
		// start := pos
		// 解析一个 token（文件名），处理转义和续行
		var token strings.Builder
		escaped := false
		// lineContinuation := false
		for pos < n {
			ch := in[pos]
			if escaped {
				// 转义字符处理
				switch ch {
				case ' ', '#', ':', '\\':
					token.WriteRune(ch)
				default:
					token.WriteRune('\\')
					token.WriteRune(ch)
				}
				escaped = false
				pos++
				continue
			}
			if ch == '\\' {
				// 检查是否续行：反斜杠后跟换行符
				if pos+1 < n && (in[pos+1] == '\n' || in[pos+1] == '\r') {
					// 续行：跳过反斜杠和换行符
					pos += 2
					// 跳过续行后的空白
					for pos < n && (in[pos] == ' ' || in[pos] == '\t') {
						pos++
					}
					// lineContinuation = true
					continue
				}
				// 普通转义
				escaped = true
				pos++
				continue
			}
			if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == ':' {
				// 分隔符，结束当前 token
				break
			}
			token.WriteRune(ch)
			pos++
		}
		// 处理 token 结束后的可能冒号或换行
		// 跳过当前 token 后的空白
		for pos < n && (in[pos] == ' ' || in[pos] == '\t') {
			pos++
		}
		// 检查是否遇到冒号
		if pos < n && in[pos] == ':' {
			// 冒号标志着从目标切换到依赖
			parsingTargets = false
			haveTarget = true
			pos++ // 跳过冒号
			// 跳过冒号后的空白
			for pos < n && (in[pos] == ' ' || in[pos] == '\t') {
				pos++
			}
			continue
		}
		// 如果 token 非空，添加到列表
		tokenStr := token.String()
		if tokenStr != "" {
			if parsingTargets {
				// 目标：去重
				found := false
				for _, out := range p.Outs {
					if out == tokenStr {
						found = true
						break
					}
				}
				if !found {
					p.Outs = append(p.Outs, tokenStr)
				}
			} else {
				// 依赖：检查是否被污染
				if poisonedInput {
					return &DepfileError{Msg: "inputs may not also have inputs"}
				}
				// 去重
				found := false
				for _, in := range p.Ins {
					if in == tokenStr {
						found = true
						break
					}
				}
				if !found {
					p.Ins = append(p.Ins, tokenStr)
				}
			}
		}
		// 处理换行符（结束当前规则）
		if pos < n && (in[pos] == '\n' || in[pos] == '\r') {
			// 跳过换行符
			if pos+1 < n && in[pos] == '\r' && in[pos+1] == '\n' {
				pos += 2
			} else {
				pos++
			}
			// 重置状态，开始下一个规则
			parsingTargets = true
			poisonedInput = false
			isFirstLine = false
			continue
		}
		// 如果行尾没有换行符但遇到 EOF，也要结束规则
		if pos >= n {
			break
		}
		// 否则继续循环，可能在同一行有多个 token
	}
	if !haveTarget && !isFirstLine {
		return &DepfileError{Msg: "expected ':' in depfile"}
	}
	return nil
}

// DepfileError 自定义错误类型
type DepfileError struct {
	Msg string
}

func (e *DepfileError) Error() string {
	return e.Msg
}
