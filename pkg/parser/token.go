package parser

// Token 类型
type Token int

const (
	T_ERROR Token = iota
	T_BUILD
	T_COLON
	T_DEFAULT
	T_EQUALS
	T_IDENT
	T_INCLUDE
	T_INDENT
	T_NEWLINE
	T_PIPE
	T_PIPE2
	T_PIPEAT
	T_POOL
	T_RULE
	T_SUBNINJA
	T_EOF
)

// TokenName 返回 token 的可读名称
func TokenName(t Token) string {
	switch t {
	case T_ERROR:
		return "lexing error"
	case T_BUILD:
		return "'build'"
	case T_COLON:
		return "':'"
	case T_DEFAULT:
		return "'default'"
	case T_EQUALS:
		return "'='"
	case T_IDENT:
		return "identifier"
	case T_INCLUDE:
		return "'include'"
	case T_INDENT:
		return "indent"
	case T_NEWLINE:
		return "newline"
	case T_PIPE2:
		return "'||'"
	case T_PIPE:
		return "'|'"
	case T_PIPEAT:
		return "'|@'"
	case T_POOL:
		return "'pool'"
	case T_RULE:
		return "'rule'"
	case T_SUBNINJA:
		return "'subninja'"
	case T_EOF:
		return "eof"
	default:
		return "unknown"
	}
}

// TokenErrorHint 返回针对特定 token 的提示信息
func TokenErrorHint(expected Token) string {
	if expected == T_COLON {
		return " ($ also escapes ':')"
	}
	return ""
}
