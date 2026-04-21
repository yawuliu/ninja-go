package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEnv 用于测试的模拟环境
type mockEnv struct {
	vars map[string]string
}

func newMockEnv() *mockEnv {
	return &mockEnv{
		vars: make(map[string]string),
	}
}

func (m *mockEnv) LookupVariable(key string) string {
	return m.vars[key]
}

func (m *mockEnv) SetVariable(key, value string) {
	m.vars[key] = value
}

// TestEvalString_Empty 测试空 EvalString
func TestEvalString_Empty(t *testing.T) {
	es := &EvalString{}
	assert.True(t, es.Empty())

	es.AddText("hello")
	assert.False(t, es.Empty())

	es.Clear()
	assert.True(t, es.Empty())
}

// TestEvalString_SingleToken 测试单 token 模式
func TestEvalString_SingleToken(t *testing.T) {
	es := &EvalString{}
	es.AddText("hello")

	// 第一个文本应该设置 singleToken
	assert.Equal(t, "hello", es.singleToken)
	assert.Empty(t, es.fragments)

	env := newMockEnv()
	result := es.Evaluate(env)
	assert.Equal(t, "hello", result)
}

// TestEvalString_MultipleTexts 测试多个文本合并
func TestEvalString_MultipleTexts(t *testing.T) {
	es := &EvalString{}
	es.AddText("hello")
	es.AddText(" ")
	es.AddText("world")

	// 多个文本应该合并到 fragments
	// 注意：第一次AddText设置singleToken，第二次触发转换，后续合并到最后一个fragment
	assert.Empty(t, es.singleToken)
	require.Len(t, es.fragments, 2)
	assert.Equal(t, "hello", es.fragments[0].Text)
	assert.Equal(t, " world", es.fragments[1].Text)
	assert.False(t, es.fragments[0].IsSpecial)
	assert.False(t, es.fragments[1].IsSpecial)

	// 验证求值结果正确
	env := newMockEnv()
	result := es.Evaluate(env)
	assert.Equal(t, "hello world", result)
}

// TestEvalString_WithVariable 测试包含变量
func TestEvalString_WithVariable(t *testing.T) {
	es := &EvalString{}
	es.AddText("gcc ")
	es.AddSpecial("in")
	es.AddText(" -o ")
	es.AddSpecial("out")

	env := newMockEnv()
	env.SetVariable("in", "foo.c")
	env.SetVariable("out", "foo.o")

	result := es.Evaluate(env)
	assert.Equal(t, "gcc foo.c -o foo.o", result)
}

// TestEvalString_NoVariable 测试变量不存在
func TestEvalString_NoVariable(t *testing.T) {
	es := &EvalString{}
	es.AddText("prefix-")
	es.AddSpecial("missing")
	es.AddText("-suffix")

	env := newMockEnv()
	// 不设置 missing 变量

	result := es.Evaluate(env)
	assert.Equal(t, "prefix--suffix", result)
}

// TestEvalString_Unparse 测试 Unparse 方法
func TestEvalString_Unparse(t *testing.T) {
	tests := []struct {
		name     string
		build    func() *EvalString
		expected string
	}{
		{
			name: "simple",
			build: func() *EvalString {
				es := &EvalString{}
				es.AddText("hello")
				return es
			},
			expected: "hello",
		},
		{
			name: "with_variable",
			build: func() *EvalString {
				es := &EvalString{}
				es.AddText("gcc ")
				es.AddSpecial("in")
				return es
			},
			expected: "gcc $in",
		},
		{
			name: "multiple_variables",
			build: func() *EvalString {
				es := &EvalString{}
				es.AddText("gcc ")
				es.AddSpecial("in")
				es.AddText(" -o ")
				es.AddSpecial("out")
				return es
			},
			expected: "gcc $in -o $out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			es := tt.build()
			result := es.Unparse()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestEvalString_Serialize 测试 Serialize 方法
func TestEvalString_Serialize(t *testing.T) {
	tests := []struct {
		name     string
		build    func() *EvalString
		expected string
	}{
		{
			name: "single_token",
			build: func() *EvalString {
				es := &EvalString{}
				es.AddText("hello")
				return es
			},
			expected: "[hello]",
		},
		{
			name: "text_only",
			build: func() *EvalString {
				es := &EvalString{}
				es.AddText("hello")
				es.AddText(" world")
				return es
			},
			expected: "[hello][ world]",
		},
		{
			name: "with_special",
			build: func() *EvalString {
				es := &EvalString{}
				es.AddText("gcc ")
				es.AddSpecial("in")
				return es
			},
			expected: "[gcc ][$in]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			es := tt.build()
			result := es.Serialize()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestEvalString_Clear 测试 Clear 方法
func TestEvalString_Clear(t *testing.T) {
	es := &EvalString{}
	es.AddText("hello")
	es.AddSpecial("var")

	assert.False(t, es.Empty())

	es.Clear()

	assert.True(t, es.Empty())
	assert.Empty(t, es.singleToken)
	assert.Empty(t, es.fragments)
}

// TestEvalString_ComplexExpression 测试复杂表达式
func TestEvalString_ComplexExpression(t *testing.T) {
	es := &EvalString{}

	// 模拟：gcc -c $in -o $out $flags
	es.AddText("gcc -c ")
	es.AddSpecial("in")
	es.AddText(" -o ")
	es.AddSpecial("out")
	es.AddText(" ")
	es.AddSpecial("flags")

	env := newMockEnv()
	env.SetVariable("in", "main.c")
	env.SetVariable("out", "main.o")
	env.SetVariable("flags", "-Wall -O2")

	result := es.Evaluate(env)
	assert.Equal(t, "gcc -c main.c -o main.o -Wall -O2", result)
}

// TestEvalString_EmptyVariable 测试空变量值
func TestEvalString_EmptyVariable(t *testing.T) {
	es := &EvalString{}
	es.AddText("before-")
	es.AddSpecial("empty")
	es.AddText("-after")

	env := newMockEnv()
	env.SetVariable("empty", "")

	result := es.Evaluate(env)
	assert.Equal(t, "before--after", result)
}

// TestEvalString_ConsecutiveVariables 测试连续变量
func TestEvalString_ConsecutiveVariables(t *testing.T) {
	es := &EvalString{}
	es.AddSpecial("a")
	es.AddSpecial("b")
	es.AddSpecial("c")

	env := newMockEnv()
	env.SetVariable("a", "1")
	env.SetVariable("b", "2")
	env.SetVariable("c", "3")

	result := es.Evaluate(env)
	assert.Equal(t, "123", result)
}

// TestEvalString_SpecialChars 测试特殊字符
func TestEvalString_SpecialChars(t *testing.T) {
	es := &EvalString{}
	es.AddText("path/to/log_file_")

	env := newMockEnv()
	result := es.Evaluate(env)
	assert.Equal(t, "path/to/log_file_", result)
}

// TestEvalString_LargeInput 测试大输入
func TestEvalString_LargeInput(t *testing.T) {
	es := &EvalString{}

	// 构建一个大的 EvalString
	for i := 0; i < 1000; i++ {
		es.AddText("x")
	}

	env := newMockEnv()
	result := es.Evaluate(env)
	assert.Len(t, result, 1000)
}

// TestEvalString_UnparseEmpty 测试 Unparse 空字符串
func TestEvalString_UnparseEmpty(t *testing.T) {
	es := &EvalString{}
	result := es.Unparse()
	assert.Equal(t, "", result)
}

// TestEvalString_Transition 测试 singleToken 到 fragments 的转换
func TestEvalString_Transition(t *testing.T) {
	es := &EvalString{}

	// 先添加文本，应该设置 singleToken
	es.AddText("first")
	assert.Equal(t, "first", es.singleToken)
	assert.Empty(t, es.fragments)

	// 添加变量，应该转换到 fragments
	es.AddSpecial("var")
	assert.Empty(t, es.singleToken)
	require.Len(t, es.fragments, 2)
	assert.Equal(t, "first", es.fragments[0].Text)
	assert.False(t, es.fragments[0].IsSpecial)
	assert.Equal(t, "var", es.fragments[1].Text)
	assert.True(t, es.fragments[1].IsSpecial)
}
