package builder

import (
	"errors"
	"fmt"
)

type Token int

const (
	ERROR Token = iota
	BUILD
	COLON
	DEFAULT
	EQUALS
	IDENT
	INCLUDE
	INDENT
	NEWLINE
	PIPE2
	PIPE
	PIPEAT
	POOL
	RULE
	SUBNINJA
	TEOF
)

func (t Token) String() string {
	switch t {
	case ERROR:
		return "lexing error"
	case BUILD:
		return "'build'"
	case COLON:
		return "':'"
	case DEFAULT:
		return "'default'"
	case EQUALS:
		return "'='"
	case IDENT:
		return "identifier"
	case INCLUDE:
		return "'include'"
	case INDENT:
		return "indent"
	case NEWLINE:
		return "newline"
	case PIPE2:
		return "'||'"
	case PIPE:
		return "'|'"
	case PIPEAT:
		return "'|@'"
	case POOL:
		return "'pool'"
	case RULE:
		return "'rule'"
	case SUBNINJA:
		return "'subninja'"
	case TEOF:
		return "eof"
	default:
		return "unknown token"
	}
}

func TokenErrorHint(expected Token) string {
	if expected == COLON {
		return " ($ also escapes ':')"
	}
	return ""
}

type Lexer struct {
	filename              string
	yyinput               string
	ofs_                  int
	lastPos               int
	manifestVersionMajor  int
	manifestVersionMinor  int
	newlineVersionChecked bool
}

func NewLexer(filename, input string) *Lexer {
	l := &Lexer{}
	l.Start("input", input)
	return l
}

func (l *Lexer) Start(filename, input string) {
	l.filename = filename
	l.yyinput = input
	l.ofs_ = 0
	l.lastPos = 0
	l.manifestVersionMajor = 1
	l.manifestVersionMinor = 14
	l.newlineVersionChecked = false
}

func (l *Lexer) SetManifestVersion(major, minor int) {
	l.manifestVersionMajor = major
	l.manifestVersionMinor = minor
}

func (l *Lexer) Error(msg string) error {
	line, col := l.lineColumnOf(l.lastPos)
	return fmt.Errorf("%s:%d:%d: %s", l.filename, line, col, msg)
}

func (l *Lexer) lineColumnOf(offset int) (int, int) {
	line, col := 1, 1
	for _, ch := range l.yyinput[:offset] {
		if ch == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func (l *Lexer) DescribeLastError() string {
	if l.lastPos < len(l.yyinput) && l.yyinput[l.lastPos] == '\t' {
		return "tabs are not allowed, use spaces"
	}
	return "lexing error"
}

func (l *Lexer) UnreadToken() {
	l.ofs_ = l.lastPos
}

func (l *Lexer) PeekToken(t Token) bool {
	tok := l.ReadToken()
	if tok == t {
		return true
	}
	l.UnreadToken()
	return false
}

var eyybm = [256]byte{
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	128, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
}

func (l *Lexer) EatWhitespace() {
	var p = l.ofs_
	var marker int
	for {
		l.ofs_ = p
		if p >= len(l.yyinput) {
			return
		}

		{
			var yych byte
			yych = l.yyinput[p]
			if eyybm[0+yych]&128 != 0 {
				goto yy59
			}
			if yych <= 0x00 {
				goto yy56
			}
			if yych == '$' {
				goto yy60
			}
			goto yy57
		yy56:
			p++
			{
				break
			}
		yy57:
			p++
		yy58:
			{
				break
			}
		yy59:
			p++
			yych = l.yyinput[p]
			if eyybm[0+yych]&128 != 0 {
				goto yy59
			}
			{
				continue
			}
		yy60:
			p++
			yych = l.yyinput[p]
			marker = p
			if yych == '\n' {
				goto yy61
			}
			if yych == '\r' {
				goto yy62
			}
			goto yy58
		yy61:
			p++
			{
				continue
			}
		yy62:
			p++
			yych = l.yyinput[p]
			if yych == '\n' {
				goto yy63
			}
			p = marker
			goto yy58
		yy63:
			p++
			{
				continue
			}
		}
	}
}

var yyybm = [256]byte{
	0, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 0, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	160, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 192, 192, 128,
	192, 192, 192, 192, 192, 192, 192, 192,
	192, 192, 128, 128, 128, 128, 128, 128,
	128, 192, 192, 192, 192, 192, 192, 192,
	192, 192, 192, 192, 192, 192, 192, 192,
	192, 192, 192, 192, 192, 192, 192, 192,
	192, 192, 192, 128, 128, 128, 128, 192,
	128, 192, 192, 192, 192, 192, 192, 192,
	192, 192, 192, 192, 192, 192, 192, 192,
	192, 192, 192, 192, 192, 192, 192, 192,
	192, 192, 192, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
}

func (l *Lexer) ReadToken() Token {
	var p = l.ofs_
	var q int
	var start int
	var token Token
	for {
		start = p

		{
			var yych byte
			yyaccept := 0
			if p >= len(l.yyinput) {
				goto yy1
			}
			yych = l.yyinput[p]
			if yyybm[0+yych]&32 != 0 {
				goto yy6
			}
			if yych <= '^' {
				if yych <= ',' {
					if yych <= '\f' {
						if yych <= 0x00 {
							goto yy1
						}
						if yych == '\n' {
							goto yy4
						}
						goto yy2
					} else {
						if yych <= '\r' {
							goto yy5
						}
						if yych == '#' {
							goto yy8
						}
						goto yy2
					}
				} else {
					if yych <= ':' {
						if yych == '/' {
							goto yy2
						}
						if yych <= '9' {
							goto yy9
						}
						goto yy11
					} else {
						if yych <= '=' {
							if yych <= '<' {
								goto yy2
							}
							goto yy12
						} else {
							if yych <= '@' {
								goto yy2
							}
							if yych <= 'Z' {
								goto yy9
							}
							goto yy2
						}
					}
				}
			} else {
				if yych <= 'i' {
					if yych <= 'b' {
						if yych == '`' {
							goto yy2
						}
						if yych <= 'a' {
							goto yy9
						}
						goto yy13
					} else {
						if yych == 'd' {
							goto yy14
						}
						if yych <= 'h' {
							goto yy9
						}
						goto yy15
					}
				} else {
					if yych <= 'r' {
						if yych == 'p' {
							goto yy16
						}
						if yych <= 'q' {
							goto yy9
						}
						goto yy17
					} else {
						if yych <= 'z' {
							if yych <= 's' {
								goto yy18
							}
							goto yy9
						} else {
							if yych == '|' {
								goto yy19
							}
							goto yy2
						}
					}
				}
			}
		yy1:
			p++
			{
				token = TEOF
				break
			}
		yy2:
			p++
		yy3:
			{
				token = ERROR
				break
			}
		yy4:
			p++
			{
				token = NEWLINE
				break
			}
		yy5:
			p++
			yych = l.yyinput[p]
			if yych == '\n' {
				goto yy20
			}
			goto yy3
		yy6:
			yyaccept = 0
			p++
			yych = l.yyinput[p]
			q = p
			if yyybm[0+yych]&32 != 0 {
				goto yy6
			}
			if yych <= '\f' {
				if yych == '\n' {
					goto yy4
				}
			} else {
				if yych <= '\r' {
					goto yy21
				}
				if yych == '#' {
					goto yy23
				}
			}
		yy7:
			{
				token = INDENT
				break
			}
		yy8:
			yyaccept = 1
			p++
			yych = l.yyinput[p]
			q = p
			if yych <= 0x00 {
				goto yy3
			}
			goto yy24
		yy9:
			p++
			yych = l.yyinput[p]
		yy10:
			if yyybm[0+yych]&64 != 0 {
				goto yy9
			}
			{
				token = IDENT
				break
			}
		yy11:
			p++
			{
				token = COLON
				break
			}
		yy12:
			p++
			{
				token = EQUALS
				break
			}
		yy13:
			p++
			yych = l.yyinput[p]
			if yych == 'u' {
				goto yy25
			}
			goto yy10
		yy14:
			p++
			yych = l.yyinput[p]
			if yych == 'e' {
				goto yy26
			}
			goto yy10
		yy15:
			p++
			yych = l.yyinput[p]
			if yych == 'n' {
				goto yy27
			}
			goto yy10
		yy16:
			p++
			yych = l.yyinput[p]
			if yych == 'o' {
				goto yy28
			}
			goto yy10
		yy17:
			p++
			yych = l.yyinput[p]
			if yych == 'u' {
				goto yy29
			}
			goto yy10
		yy18:
			p++
			yych = l.yyinput[p]
			if yych == 'u' {
				goto yy30
			}
			goto yy10
		yy19:
			p++
			yych = l.yyinput[p]
			if yych == '@' {
				goto yy31
			}
			if yych == '|' {
				goto yy32
			}
			{
				token = PIPE
				break
			}
		yy20:
			p++
			{
				token = NEWLINE
				break
			}
		yy21:
			p++
			yych = l.yyinput[p]
			if yych == '\n' {
				goto yy20
			}
		yy22:
			p = q
			if yyaccept == 0 {
				goto yy7
			} else {
				goto yy3
			}
		yy23:
			p++
			yych = l.yyinput[p]
		yy24:
			if yyybm[0+yych]&128 != 0 {
				goto yy23
			}
			if yych <= 0x00 {
				goto yy22
			}
			p++
			{
				continue
			}
		yy25:
			p++
			yych = l.yyinput[p]
			if yych == 'i' {
				goto yy33
			}
			goto yy10
		yy26:
			p++
			yych = l.yyinput[p]
			if yych == 'f' {
				goto yy34
			}
			goto yy10
		yy27:
			p++
			yych = l.yyinput[p]
			if yych == 'c' {
				goto yy35
			}
			goto yy10
		yy28:
			p++
			yych = l.yyinput[p]
			if yych == 'o' {
				goto yy36
			}
			goto yy10
		yy29:
			p++
			yych = l.yyinput[p]
			if yych == 'l' {
				goto yy37
			}
			goto yy10
		yy30:
			p++
			yych = l.yyinput[p]
			if yych == 'b' {
				goto yy38
			}
			goto yy10
		yy31:
			p++
			{
				token = PIPEAT
				break
			}
		yy32:
			p++
			{
				token = PIPE2
				break
			}
		yy33:
			p++
			yych = l.yyinput[p]
			if yych == 'l' {
				goto yy39
			}
			goto yy10
		yy34:
			p++
			yych = l.yyinput[p]
			if yych == 'a' {
				goto yy40
			}
			goto yy10
		yy35:
			p++
			yych = l.yyinput[p]
			if yych == 'l' {
				goto yy41
			}
			goto yy10
		yy36:
			p++
			yych = l.yyinput[p]
			if yych == 'l' {
				goto yy42
			}
			goto yy10
		yy37:
			p++
			yych = l.yyinput[p]
			if yych == 'e' {
				goto yy43
			}
			goto yy10
		yy38:
			p++
			yych = l.yyinput[p]
			if yych == 'n' {
				goto yy44
			}
			goto yy10
		yy39:
			p++
			yych = l.yyinput[p]
			if yych == 'd' {
				goto yy45
			}
			goto yy10
		yy40:
			p++
			yych = l.yyinput[p]
			if yych == 'u' {
				goto yy46
			}
			goto yy10
		yy41:
			p++
			yych = l.yyinput[p]
			if yych == 'u' {
				goto yy47
			}
			goto yy10
		yy42:
			p++
			yych = l.yyinput[p]
			if yyybm[0+yych]&64 != 0 {
				goto yy9
			}
			{
				token = POOL
				break
			}
		yy43:
			p++
			yych = l.yyinput[p]
			if yyybm[0+yych]&64 != 0 {
				goto yy9
			}
			{
				token = RULE
				break
			}
		yy44:
			p++
			yych = l.yyinput[p]
			if yych == 'i' {
				goto yy48
			}
			goto yy10
		yy45:
			p++
			yych = l.yyinput[p]
			if yyybm[0+yych]&64 != 0 {
				goto yy9
			}
			{
				token = BUILD
				break
			}
		yy46:
			p++
			yych = l.yyinput[p]
			if yych == 'l' {
				goto yy49
			}
			goto yy10
		yy47:
			p++
			yych = l.yyinput[p]
			if yych == 'd' {
				goto yy50
			}
			goto yy10
		yy48:
			p++
			yych = l.yyinput[p]
			if yych == 'n' {
				goto yy51
			}
			goto yy10
		yy49:
			p++
			yych = l.yyinput[p]
			if yych == 't' {
				goto yy52
			}
			goto yy10
		yy50:
			p++
			yych = l.yyinput[p]
			if yych == 'e' {
				goto yy53
			}
			goto yy10
		yy51:
			p++
			yych = l.yyinput[p]
			if yych == 'j' {
				goto yy54
			}
			goto yy10
		yy52:
			p++
			yych = l.yyinput[p]
			if yyybm[0+yych]&64 != 0 {
				goto yy9
			}
			{
				token = DEFAULT
				break
			}
		yy53:
			p++
			yych = l.yyinput[p]
			if yyybm[0+yych]&64 != 0 {
				goto yy9
			}
			{
				token = INCLUDE
				break
			}
		yy54:
			p++
			yych = l.yyinput[p]
			if yych != 'a' {
				goto yy10
			}
			p++
			yych = l.yyinput[p]
			if yyybm[0+yych]&64 != 0 {
				goto yy9
			}
			{
				token = SUBNINJA
				break
			}
		}
	}

	l.lastPos = start
	l.ofs_ = p
	if token != NEWLINE && token != TEOF {
		l.EatWhitespace()
	}
	return token
}

var yybm = [256]byte{
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 128, 128, 0,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 0, 0, 0, 0, 0, 0,
	0, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 0, 0, 0, 0, 128,
	0, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
}

func (l *Lexer) ReadIdent(out *string) bool {
	var p = l.ofs_
	var start int
	for {
		start = p
		if p >= len(l.yyinput) {
			l.lastPos = start
			return false
		}
		{
			var yych byte
			yych = l.yyinput[p]
			if yybm[0+yych]&128 != 0 {
				goto yy65
			}
			p++
			{
				l.lastPos = start
				return false
			}
		yy65:
			p++
			if p >= len(l.yyinput) {
				*out = l.yyinput[start:p]
				break
			}
			yych = l.yyinput[p]
			if yybm[0+yych]&128 != 0 {
				goto yy65
			}
			{
				*out = l.yyinput[start:p]
				break
			}
		}
	}
	l.lastPos = start
	l.ofs_ = p
	l.EatWhitespace()
	return true
}

var myybm = [256]byte{
	0, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 0, 16, 16, 0, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	32, 16, 16, 16, 0, 16, 16, 16,
	16, 16, 16, 16, 16, 208, 144, 16,
	208, 208, 208, 208, 208, 208, 208, 208,
	208, 208, 0, 16, 16, 16, 16, 16,
	16, 208, 208, 208, 208, 208, 208, 208,
	208, 208, 208, 208, 208, 208, 208, 208,
	208, 208, 208, 208, 208, 208, 208, 208,
	208, 208, 208, 16, 16, 16, 16, 208,
	16, 208, 208, 208, 208, 208, 208, 208,
	208, 208, 208, 208, 208, 208, 208, 208,
	208, 208, 208, 208, 208, 208, 208, 208,
	208, 208, 208, 16, 0, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
}

// $^ supported starting this version
const kMinNewlineEscapeVersionMajor = 1
const kMinNewlineEscapeVersionMinor = 14

func (l *Lexer) ReadEvalString(eval *EvalString, path bool) error {
	var p = l.ofs_
	var marker int
	var start int
	for {
		start = p
		if p >= len(l.yyinput) {
			l.lastPos = start
			return errors.New("unexpected EOF")
		}

		{
			var yych byte
			yych = l.yyinput[p]
			if myybm[0+yych]&16 != 0 {
				goto yy68
			}
			if yych <= '\r' {
				if yych <= 0x00 {
					l.lastPos = start
					return errors.New("unexpected EOF")
				}
				if yych <= '\n' {
					goto yy69
				}
				goto yy70
			} else {
				if yych <= ' ' {
					goto yy69
				}
				if yych <= '$' {
					goto yy71
				}
				goto yy69
			}
		yy68:
			p++
			yych = l.yyinput[p]
			if myybm[0+yych]&16 != 0 {
				goto yy68
			}
			{
				eval.AddText(l.yyinput[start:p])
				continue
			}
		yy69:
			p++
			{
				if path {
					p = start
					break
				} else {
					if l.yyinput[start] == '\n' {
						break
					}
					eval.AddText(l.yyinput[start : start+1])
					continue
				}
			}
		yy70:
			p++
			yych = l.yyinput[p]
			if yych == '\n' {
				goto yy72
			}
			{
				l.lastPos = start
				return errors.New(l.DescribeLastError())
			}
		yy71:
			p++
			yych = l.yyinput[p]
			if myybm[0+yych]&64 != 0 {
				goto yy79
			}
			if yych <= '#' {
				if yych <= '\f' {
					if yych == '\n' {
						goto yy75
					}
					goto yy73
				} else {
					if yych <= '\r' {
						goto yy76
					}
					if yych == ' ' {
						goto yy77
					}
					goto yy73
				}
			} else {
				if yych <= ']' {
					if yych <= '$' {
						goto yy78
					}
					if yych <= '/' {
						goto yy73
					}
					if yych <= ':' {
						goto yy80
					}
					goto yy73
				} else {
					if yych <= '^' {
						goto yy81
					}
					if yych <= '`' {
						goto yy73
					}
					if yych <= '{' {
						goto yy82
					}
					goto yy73
				}
			}
		yy72:
			p++
			{
				if path {
					p = start
				}
				break
			}
		yy73:
			p++
		yy74:
			{
				l.lastPos = start
				return errors.New("bad $-escape (literal $ must be written as $$)")
			}
		yy75:
			p++
			yych = l.yyinput[p]
			if myybm[0+yych]&32 != 0 {
				goto yy75
			}
			{
				continue
			}
		yy76:
			p++
			yych = l.yyinput[p]
			if yych == '\n' {
				goto yy83
			}
			goto yy74
		yy77:
			p++
			{
				eval.AddText(" ")
				continue
			}
		yy78:
			p++
			{
				eval.AddText("$")
				continue
			}
		yy79:
			p++
			yych = l.yyinput[p]
			if myybm[0+yych]&64 != 0 {
				goto yy79
			}
			{
				eval.AddSpecial(l.yyinput[start+1 : p])
				continue
			}
		yy80:
			p++
			{
				eval.AddText(":")
				continue
			}
		yy81:
			p++
			{
				if !l.newlineVersionChecked {
					if (l.manifestVersionMajor < kMinNewlineEscapeVersionMajor) ||
						(l.manifestVersionMajor == kMinNewlineEscapeVersionMajor &&
							l.manifestVersionMinor < kMinNewlineEscapeVersionMinor) {
						return errors.New("using $^ escape requires specifying 'ninja_required_version' with version greater or equal 1.14")
					}
					l.newlineVersionChecked = true
				}
				eval.AddText("\n")
				continue
			}
		yy82:
			p++
			yych = l.yyinput[p]
			marker = p
			if myybm[0+yych]&128 != 0 {
				goto yy84
			}
			goto yy74
		yy83:
			p++
			yych = l.yyinput[p]
			if yych == ' ' {
				goto yy83
			}
			{
				continue
			}
		yy84:
			p++
			yych = l.yyinput[p]
			if myybm[0+yych]&128 != 0 {
				goto yy84
			}
			if yych == '}' {
				goto yy85
			}
			p = marker
			goto yy74
		yy85:
			p++
			{
				eval.AddSpecial(l.yyinput[start+2 : p-1])
				continue
			}
		}
	}
	l.lastPos = start
	l.ofs_ = p
	if path {
		l.EatWhitespace()
	}
	// Non-path strings end in newlines, so there's no whitespace to eat.
	return nil
}

// / Read a path (complete with $escapes).
// / Returns false only on error, returned path may be empty if a delimiter
// / (space, newline) is hit.
func (l *Lexer) ReadPath(path *EvalString) (bool, error) {
	err := l.ReadEvalString(path, true)
	if err != nil {
		return false, err
	}
	return true, nil
}

// / Read the value side of a var = value line (complete with $escapes).
// / Returns false only on error.
func (l *Lexer) ReadVarValue(value *EvalString) (bool, error) {
	err := l.ReadEvalString(value, false)
	if err != nil {
		return false, err
	}
	return true, nil
}
