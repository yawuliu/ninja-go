package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewRule 测试规则创建
func TestNewRule(t *testing.T) {
	rule := NewRule("cc")
	assert.NotNil(t, rule)
	assert.Equal(t, "cc", rule.Name)
	assert.False(t, rule.Phony)
	assert.NotNil(t, rule.Bindings)
	assert.Empty(t, rule.Bindings)
}

// TestPhonyRule 测试 Phony 规则创建
func TestPhonyRule(t *testing.T) {
	rule := PhonyRule()
	assert.NotNil(t, rule)
	assert.Equal(t, "phony", rule.Name)
	assert.True(t, rule.Phony)
	assert.NotNil(t, rule.Bindings)
}

// TestRule_IsPhony 测试 IsPhony 方法
func TestRule_IsPhony(t *testing.T) {
	ccRule := NewRule("cc")
	assert.False(t, ccRule.IsPhony())

	phonyRule := PhonyRule()
	assert.True(t, phonyRule.IsPhony())
}

// TestRule_AddBinding 测试添加绑定
func TestRule_AddBinding(t *testing.T) {
	rule := NewRule("cc")

	eval := &EvalString{}
	eval.AddText("gcc $in -o $out")

	rule.AddBinding("command", eval)

	assert.Len(t, rule.Bindings, 1)
	assert.Equal(t, eval, rule.Bindings["command"])
}

// TestRule_GetBinding 测试获取绑定
func TestRule_GetBinding(t *testing.T) {
	rule := NewRule("cc")

	// 获取不存在的绑定
	result := rule.GetBinding("nonexistent")
	assert.Nil(t, result)

	// 添加并获取
	eval := &EvalString{}
	eval.AddText("gcc")
	rule.AddBinding("command", eval)

	result = rule.GetBinding("command")
	assert.Equal(t, eval, result)
}

// TestRule_GetBinding_Multiple 测试多个绑定
func TestRule_GetBinding_Multiple(t *testing.T) {
	rule := NewRule("cc")

	cmdEval := &EvalString{}
	cmdEval.AddText("gcc $in -o $out")
	rule.AddBinding("command", cmdEval)

	descEval := &EvalString{}
	descEval.AddText("CC $out")
	rule.AddBinding("description", descEval)

	depEval := &EvalString{}
	depEval.AddText("$out.d")
	rule.AddBinding("depfile", depEval)

	assert.Len(t, rule.Bindings, 3)
	assert.Equal(t, cmdEval, rule.GetBinding("command"))
	assert.Equal(t, descEval, rule.GetBinding("description"))
	assert.Equal(t, depEval, rule.GetBinding("depfile"))
}

// TestIsReservedBinding 测试保留绑定检查
func TestIsReservedBinding(t *testing.T) {
	// 保留绑定
	assert.True(t, IsReservedBinding("command"))
	assert.True(t, IsReservedBinding("depfile"))
	assert.True(t, IsReservedBinding("dyndep"))
	assert.True(t, IsReservedBinding("description"))
	assert.True(t, IsReservedBinding("deps"))
	assert.True(t, IsReservedBinding("generator"))
	assert.True(t, IsReservedBinding("pool"))
	assert.True(t, IsReservedBinding("restat"))
	assert.True(t, IsReservedBinding("rspfile"))
	assert.True(t, IsReservedBinding("rspfile_content"))
	assert.True(t, IsReservedBinding("msvc_deps_prefix"))

	// 非保留绑定
	assert.False(t, IsReservedBinding("custom"))
	assert.False(t, IsReservedBinding("flags"))
	assert.False(t, IsReservedBinding(""))
}

// TestRule_BindingEvaluation 测试绑定求值
func TestRule_BindingEvaluation(t *testing.T) {
	rule := NewRule("cc")
	env := &mockEnv{
		vars: map[string]string{
			"in":  "foo.c",
			"out": "foo.o",
		},
	}

	eval := &EvalString{}
	eval.AddText("gcc ")
	eval.AddSpecial("in")
	eval.AddText(" -o ")
	eval.AddSpecial("out")

	rule.AddBinding("command", eval)

	// 求值绑定
	result := rule.GetBinding("command").Evaluate(env)
	assert.Equal(t, "gcc foo.c -o foo.o", result)
}

// TestRule_EmptyBinding 测试空绑定
func TestRule_EmptyBinding(t *testing.T) {
	rule := NewRule("cc")

	// 添加空绑定
	eval := &EvalString{}
	rule.AddBinding("command", eval)

	result := rule.GetBinding("command")
	assert.NotNil(t, result)
	assert.True(t, result.Empty())
}

// TestRule_OverwriteBinding 测试覆盖绑定
func TestRule_OverwriteBinding(t *testing.T) {
	rule := NewRule("cc")

	eval1 := &EvalString{}
	eval1.AddText("gcc")
	rule.AddBinding("command", eval1)

	eval2 := &EvalString{}
	eval2.AddText("clang")
	rule.AddBinding("command", eval2)

	result := rule.GetBinding("command")
	assert.Equal(t, eval2, result)
}
