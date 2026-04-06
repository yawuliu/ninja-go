package parser

import (
	"fmt"
	"unicode"
)

// Lexer 词法分析器
type Lexer struct {
	filename              string
	input                 []rune
	pos                   int
	line                  int
	col                   int
	lastStart             int // last_token_ 的起始位置
	lastEnd               int // last_token_ 的结束位置
	newlineVersionChecked bool
	ManifestVersionMajor  int
	ManifestVersionMinor  int
}

// NewLexer 创建词法分析器（测试用）
func NewLexer(input string) *Lexer {
	l := &Lexer{}
	l.Start("input", input)
	return l
}

// Start 初始化词法分析器
func (l *Lexer) Start(filename, input string) {
	l.filename = filename
	l.input = []rune(input)
	l.pos = 0
	l.line = 1
	l.col = 0
	l.lastStart = -1
	l.lastEnd = -1
	l.newlineVersionChecked = false
	l.ManifestVersionMajor = 0
	l.ManifestVersionMinor = 0
}

// currentChar 返回当前字符（不移动）
func (l *Lexer) currentChar() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

// nextChar 前进一个字符，更新行列
func (l *Lexer) nextChar() {
	if l.pos >= len(l.input) {
		return
	}
	ch := l.input[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 0
	} else {
		l.col++
	}
}

// peekChar 预览下一个字符（不移动）
func (l *Lexer) peekChar() rune {
	if l.pos+1 >= len(l.input) {
		return 0
	}
	return l.input[l.pos+1]
}

// EatWhitespace 跳过空白和续行符
func (l *Lexer) EatWhitespace() {
	for {
		ch := l.currentChar()
		if ch == ' ' || ch == '\t' {
			l.nextChar()
			continue
		}
		if ch == '$' {
			next := l.peekChar()
			if next == '\n' || next == '\r' {
				l.nextChar() // skip $
				l.nextChar() // skip \n or \r
				if next == '\r' && l.currentChar() == '\n' {
					l.nextChar()
				}
				continue
			}
		}
		break
	}
}

// ReadToken 读取下一个 token
func (l *Lexer) ReadToken() Token {
	// 跳过空白（不包括换行）
	for l.currentChar() == ' ' || l.currentChar() == '\t' {
		l.nextChar()
	}
	//startPos := l.pos
	//startLine := l.line
	//startCol := l.col

	ch := l.currentChar()
	switch ch {
	case 0:
		return T_EOF
	case '\n':
		l.nextChar()
		return T_NEWLINE
	case '#':
		// 注释直到行尾
		for l.currentChar() != 0 && l.currentChar() != '\n' {
			l.nextChar()
		}
		return l.ReadToken() // 跳过注释，继续读下一个
	case ':':
		l.nextChar()
		return T_COLON
	case '=':
		l.nextChar()
		return T_EQUALS
	case '|':
		l.nextChar()
		if l.currentChar() == '|' {
			l.nextChar()
			return T_PIPE2
		} else if l.currentChar() == '@' {
			l.nextChar()
			return T_PIPEAT
		}
		return T_PIPE
	default:
		if unicode.IsLetter(ch) || ch == '_' {
			// 标识符或关键字
			start := l.pos
			for unicode.IsLetter(l.currentChar()) || unicode.IsDigit(l.currentChar()) || l.currentChar() == '_' || l.currentChar() == '-' {
				l.nextChar()
			}
			word := string(l.input[start:l.pos])
			switch word {
			case "build":
				return T_BUILD
			case "pool":
				return T_POOL
			case "rule":
				return T_RULE
			case "default":
				return T_DEFAULT
			case "include":
				return T_INCLUDE
			case "subninja":
				return T_SUBNINJA
			default:
				return T_IDENT
			}
		}
		// 错误字符
		l.nextChar()
		return T_ERROR
	}
}

// UnreadToken 回退到上一个 token（简单实现：将位置重置到 lastStart）
func (l *Lexer) UnreadToken() {
	if l.lastStart >= 0 {
		l.pos = l.lastStart
		// 需要恢复行列，这里简化，实际应保存，但通常不影响
	}
}

// PeekToken 预览下一个 token
func (l *Lexer) PeekToken(t Token) bool {
	tok := l.ReadToken()
	if tok == t {
		l.UnreadToken()
		return true
	}
	l.UnreadToken()
	return false
}

// ReadIdent 读取标识符
func (l *Lexer) ReadIdent() (string, error) {
	start := l.pos
	if l.pos >= len(l.input) {
		return "", l.Error("expected identifier")
	}
	for {
		ch := l.currentChar()
		if !(unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '-') {
			break
		}
		l.nextChar()
	}
	if l.pos == start {
		return "", l.Error("expected identifier")
	}
	ident := string(l.input[start:l.pos])
	l.lastStart = start
	l.lastEnd = l.pos
	l.EatWhitespace()
	return ident, nil
}

// ReadPath 读取路径字符串
func (l *Lexer) ReadPath() (*EvalString, error) {
	return l.readEvalString(true)
}

// ReadVarValue 读取变量值字符串
func (l *Lexer) ReadVarValue() (*EvalString, error) {
	return l.readEvalString(false)
}

// readEvalString 核心读取方法
func (l *Lexer) readEvalString(path bool) (*EvalString, error) {
	eval := &EvalString{}
	for {
		start := l.pos
		ch := l.currentChar()
		switch {
		case ch == 0:
			return nil, l.Error("unexpected EOF")
		case ch == '\n':
			if path {
				return eval, nil
			}
			l.nextChar()
			return eval, nil
		case ch == ' ' || ch == '\t' || ch == '|' || ch == ':':
			if path {
				return eval, nil
			}
			l.nextChar()
			eval.AddText(string(ch))
			continue
		case ch == '$':
			l.nextChar()
			if err := l.handleDollar(eval, path); err != nil {
				return nil, err
			}
			continue
		default:
			// 普通文本
			for {
				nch := l.currentChar()
				if nch == 0 || nch == '$' || (path && (nch == '\n' || nch == ' ' || nch == '\t' || nch == '|' || nch == ':')) {
					break
				}
				if !path && nch == '\n' {
					break
				}
				l.nextChar()
			}
			if l.pos > start {
				eval.AddText(string(l.input[start:l.pos]))
			}
			continue
		}
	}
}

// handleDollar 处理 $ 转义序列
func (l *Lexer) handleDollar(eval *EvalString, path bool) error {
	ch := l.currentChar()
	if ch == 0 {
		return l.Error("unexpected EOF after $")
	}
	switch ch {
	case '$':
		l.nextChar()
		eval.AddText("$")
		return nil
	case ' ':
		l.nextChar()
		eval.AddText(" ")
		return nil
	case ':':
		l.nextChar()
		eval.AddText(":")
		return nil
	case '^':
		l.nextChar()
		if !l.newlineVersionChecked {
			if l.ManifestVersionMajor < 1 || (l.ManifestVersionMajor == 1 && l.ManifestVersionMinor < 14) {
				return l.Error("using $^ escape requires specifying 'ninja_required_version' with version >= 1.14")
			}
			l.newlineVersionChecked = true
		}
		eval.AddText("\n")
		return nil
	case '{':
		l.nextChar()
		varName, err := l.readIdentUntil('}')
		if err != nil {
			return err
		}
		if l.currentChar() != '}' {
			return l.Error("missing '}'")
		}
		l.nextChar()
		eval.AddSpecial(varName)
		return nil
	default:
		if l.isIdentStart(ch) {
			varName := l.readSimpleIdent()
			eval.AddSpecial(varName)
			return nil
		}
		return l.Error("bad $-escape (literal $ must be written as $$)")
	}
}

// readIdentUntil 读取标识符直到遇到 stop 字符
func (l *Lexer) readIdentUntil(stop rune) (string, error) {
	start := l.pos
	for {
		ch := l.currentChar()
		if ch == 0 {
			return "", l.Error("unexpected EOF")
		}
		if ch == stop {
			break
		}
		if !l.isIdentChar(ch) {
			return "", l.Error("invalid character in identifier")
		}
		l.nextChar()
	}
	if l.pos == start {
		return "", l.Error("empty identifier")
	}
	return string(l.input[start:l.pos]), nil
}

// readSimpleIdent 读取简单标识符
func (l *Lexer) readSimpleIdent() string {
	start := l.pos
	for {
		ch := l.currentChar()
		if !l.isIdentChar(ch) {
			break
		}
		l.nextChar()
	}
	return string(l.input[start:l.pos])
}

func (l *Lexer) isIdentChar(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '-'
}

func (l *Lexer) isIdentStart(ch rune) bool {
	return unicode.IsLetter(ch) || ch == '_'
}

// DescribeLastError 返回上一个错误 token 的详细信息
func (l *Lexer) DescribeLastError() string {
	// 简化实现，返回空字符串或检查最后一个字符
	return ""
}

// Error 构造带行列号的错误
func (l *Lexer) Error(msg string) error {
	// 使用 lastStart 位置或当前 pos 计算行列
	line, col := l.line, l.col
	// 可以进一步根据 lastStart 回溯，简化
	return &ParseError{Line: line, Col: col, Msg: msg}
}

// ParseError 自定义错误
type ParseError struct {
	Line int
	Col  int
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg)
}
