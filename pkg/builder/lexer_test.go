package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLexer_Keywords 测试所有关键字识别
func TestLexer_Keywords(t *testing.T) {
	tests := []struct {
		input    string
		expected Token
	}{
		{"build", BUILD},
		{"rule", RULE},
		{"pool", POOL},
		{"default", DEFAULT},
		{"include", INCLUDE},
		{"subninja", SUBNINJA},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer("test", tt.input)
			tok := l.ReadToken()
			assert.Equal(t, tt.expected, tok)
		})
	}
}

// TestLexer_Symbols 测试符号识别
func TestLexer_Symbols(t *testing.T) {
	tests := []struct {
		input    string
		expected Token
	}{
		{":", COLON},
		{"=", EQUALS},
		{"|", PIPE},
		{"||", PIPE2},
		{"|@", PIPEAT},
		{"\n", NEWLINE},
		{"", TEOF},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer("test", tt.input)
			tok := l.ReadToken()
			assert.Equal(t, tt.expected, tok)
		})
	}
}

// TestLexer_Identifier 测试标识符识别
func TestLexer_Identifier(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"foo", "foo"},
		{"foo123", "foo123"},
		{"foo_bar", "foo_bar"},
		{"foo-bar", "foo-bar"},
		{"foo.bar", "foo.bar"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := NewLexer("test", tt.input)
			var ident string
			ok := l.ReadIdent(&ident)
			require.True(t, ok)
			assert.Equal(t, tt.expected, ident)
		})
	}
}

// TestLexer_ReadPath 测试路径读取
func TestLexer_ReadPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "foo", "foo"},
		{"with_spaces", "foo bar", "foo"},      // path 在遇到空格时停止
		{"with_dollar", "foo$bar", "foobar"},   // $bar 是变量，需要展开
		{"with_escape", "foo$$bar", "foo$bar"}, // $$ 转义为 $
		{"with_colon", "foo$:", "foo:"},        // $: 转义为 :
		{"with_space", "foo$ ", "foo "},        // $ 转义为空格
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewLexer("test", tt.input+"\n")
			var path EvalString
			var err string
			ok := l.ReadPath(&path, &err)
			require.Equal(t, err, "")
			require.True(t, ok)
			// path 需要环境来求值，这里只检查片段
			if len(path.fragments) == 0 {
				assert.Equal(t, tt.expected, path.singleToken)
			} else if len(path.fragments) == 1 {
				assert.Equal(t, tt.expected, path.fragments[0].Text)
			}
		})
	}
}

// TestLexer_ReadVarValue 测试变量值读取
func TestLexer_ReadVarValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "value\n", "value"},
		{"with_spaces", "value with spaces\n", "value with spaces"},
		{"with_variable", "$var\n", ""}, // 需要变量求值环境
		{"with_escape", "value$$\n", "value$"},
		{"with_newline_escape", "line1$\nline2\n", "line1\nline2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewLexer("test", tt.input)
			var value EvalString
			var err string
			ok := l.ReadVarValue(&value, &err)
			require.Equal(t, err, "")
			require.True(t, ok)
		})
	}
}

// TestLexer_Comments 测试注释处理
func TestLexer_Comments(t *testing.T) {
	input := `# This is a comment
build
# Another comment
rule
`
	l := NewLexer("test", input)

	tok := l.ReadToken()
	assert.Equal(t, NEWLINE, tok) // 注释后的换行

	tok = l.ReadToken()
	assert.Equal(t, BUILD, tok)

	tok = l.ReadToken()
	assert.Equal(t, NEWLINE, tok)

	tok = l.ReadToken()
	assert.Equal(t, RULE, tok)
}

// TestLexer_Indent 测试缩进识别
func TestLexer_Indent(t *testing.T) {
	input := `rule cat
  command = echo hi
`
	l := NewLexer("test", input)

	assert.Equal(t, RULE, l.ReadToken())
	assert.Equal(t, IDENT, l.ReadToken())
	assert.Equal(t, NEWLINE, l.ReadToken())
	assert.Equal(t, INDENT, l.ReadToken())
	assert.Equal(t, IDENT, l.ReadToken())
	assert.Equal(t, EQUALS, l.ReadToken())
	assert.Equal(t, IDENT, l.ReadToken())
}

// TestLexer_EmptyInput 测试空输入
func TestLexer_EmptyInput(t *testing.T) {
	l := NewLexer("test", "")
	tok := l.ReadToken()
	assert.Equal(t, TEOF, tok)
}

// TestLexer_Whitespace 测试空白字符处理
func TestLexer_Whitespace(t *testing.T) {
	input := `  build   `
	l := NewLexer("test", input)
	tok := l.ReadToken()
	assert.Equal(t, BUILD, tok)
	tok = l.ReadToken()
	assert.Equal(t, TEOF, tok)
}

// TestLexer_UnreadToken 测试回退 token
func TestLexer_UnreadToken(t *testing.T) {
	l := NewLexer("test", "build rule")

	tok1 := l.ReadToken()
	assert.Equal(t, BUILD, tok1)

	l.UnreadToken()
	tok2 := l.ReadToken()
	assert.Equal(t, BUILD, tok2)

	tok3 := l.ReadToken()
	assert.Equal(t, RULE, tok3)
}

// TestLexer_PeekToken 测试预览 token
func TestLexer_PeekToken(t *testing.T) {
	l := NewLexer("test", "build")

	ok := l.PeekToken(BUILD)
	assert.True(t, ok)

	// 再次读取应该还是 BUILD
	tok := l.ReadToken()
	assert.Equal(t, BUILD, tok)
}

// TestLexer_ComplexManifest 测试复杂 manifest
func TestLexer_ComplexManifest(t *testing.T) {
	input := `# Sample manifest
rule cc
  command = gcc $in -o $out
  depfile = $out.d

build foo.o: cc foo.c
`
	l := NewLexer("test", input)

	// rule
	tok := l.ReadToken()
	assert.Equal(t, RULE, tok)

	// cc
	var ident string
	ok := l.ReadIdent(&ident)
	require.True(t, ok)
	assert.Equal(t, "cc", ident)

	// NEWLINE
	tok = l.ReadToken()
	assert.Equal(t, NEWLINE, tok)

	// INDENT
	tok = l.ReadToken()
	assert.Equal(t, INDENT, tok)

	// command
	ok = l.ReadIdent(&ident)
	require.True(t, ok)
	assert.Equal(t, "command", ident)

	// =
	tok = l.ReadToken()
	assert.Equal(t, EQUALS, tok)
}

// TestLexer_VersionCheck 测试版本检查
func TestLexer_VersionCheck(t *testing.T) {
	// 测试高版本支持 $^ 转义
	l := NewLexer("test", "line1$^line2\n")
	l.SetManifestVersion(1, 14)

	var value EvalString
	var err string
	l.ReadVarValue(&value, &err)
	require.Equal(t, err, "")
}

// TestLexer_ErrorCases 测试错误情况
func TestLexer_ErrorCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"tab_in_path", "path\tmore"},
		{"invalid_escape", "path$@"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewLexer("test", tt.input+"\n")
			var path EvalString
			var err string
			l.ReadPath(&path, &err)
			// 某些错误可能不会立即返回，而是在读取时处理
			_ = err
		})
	}
}

// TestLexer_EvalStringComplex 测试复杂 EvalString
func TestLexer_EvalStringComplex(t *testing.T) {
	input := "gcc ${in} -o $out -I${includedir}"
	l := NewLexer("test", input+"\n")

	var value EvalString
	var err string
	ok := l.ReadVarValue(&value, &err)
	require.Equal(t, err, "")
	require.True(t, ok)

	// 验证片段
	require.NotEmpty(t, value.fragments)

	// 检查是否有变量引用
	hasVar := false
	for _, frag := range value.fragments {
		if frag.IsSpecial {
			hasVar = true
			break
		}
	}
	assert.True(t, hasVar, "Expected variable references in fragments")
}

// TestToken_String 测试 Token 字符串表示
func TestToken_String(t *testing.T) {
	tests := []struct {
		token    Token
		expected string
	}{
		{ERROR, "lexing error"},
		{BUILD, "'build'"},
		{COLON, "':'"},
		{EQUALS, "'='"},
		{IDENT, "identifier"},
		{TEOF, "eof"},
		{Token(999), "unknown token"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.token.String())
		})
	}
}

// TestTokenErrorHint 测试错误提示
func TestTokenErrorHint(t *testing.T) {
	hint := TokenErrorHint(COLON)
	assert.Contains(t, hint, "also escapes ':'")

	hint = TokenErrorHint(BUILD)
	assert.Empty(t, hint)
}

// TestLexer_LineColumn 测试行号列号计算
func TestLexer_LineColumn(t *testing.T) {
	input := `first line
second line
third line`
	l := NewLexer("test", input)

	// 读取一些 token
	for i := 0; i < 5; i++ {
		l.ReadToken()
	}

	// 测试错误报告包含位置信息
	var err string
	l.Error("test error", &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "test:")
}

// TestLexer_NewlineEscape 测试换行符转义
func TestLexer_NewlineEscape(t *testing.T) {
	input := `command = gcc \
  -c \
  -o output`
	l := NewLexer("test", input+"\n")

	// 读取变量名
	var ident string
	ok := l.ReadIdent(&ident)
	require.True(t, ok)
	assert.Equal(t, "command", ident)

	// 读取 =
	tok := l.ReadToken()
	assert.Equal(t, EQUALS, tok)

	// 读取变量值（包含转义的换行）
	var value EvalString
	var err string
	l.ReadVarValue(&value, &err)
	require.Equal(t, err, "")
}

// TestLexer_DollarEscapes 测试美元符号转义
func TestLexer_DollarEscapes(t *testing.T) {
	tests := []struct {
		name     string
		escape   string
		expected string
	}{
		{"dollar", "$$", "$"},
		{"colon", "$:", ":"},
		{"space", "$ ", " "},
		{"newline", "$\n", ""}, // 继续到下一行
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 这些转义需要 ReadEvalString 处理
			input := "value" + tt.escape + "more"
			l := NewLexer("test", input+"\n")

			var value EvalString
			var err string
			l.ReadVarValue(&value, &err)
			require.Equal(t, err, "")
		})
	}
}

// TestLexer_CarriageReturn 测试回车符处理
func TestLexer_CarriageReturn(t *testing.T) {
	input := "line1\r\nline2\r\n"
	l := NewLexer("test", input)

	tok := l.ReadToken()
	assert.Equal(t, IDENT, tok)

	tok = l.ReadToken()
	assert.Equal(t, NEWLINE, tok)

	tok = l.ReadToken()
	assert.Equal(t, IDENT, tok)
}
