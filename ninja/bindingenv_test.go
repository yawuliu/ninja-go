package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewBindingEnv 测试环境创建
func TestNewBindingEnv(t *testing.T) {
	env := NewBindingEnv(nil)
	assert.NotNil(t, env)
	assert.Nil(t, env.parent)
	assert.NotNil(t, env.vars)
	assert.NotNil(t, env.rules)
	assert.Empty(t, env.vars)
	assert.Empty(t, env.rules)
}

// TestNewBindingEnv_WithParent 测试带父环境创建
func TestNewBindingEnv_WithParent(t *testing.T) {
	parent := NewBindingEnv(nil)
	parent.AddBinding("key", "parent_value")

	child := NewBindingEnv(parent)
	assert.NotNil(t, child)
	assert.Equal(t, parent, child.parent)

	// 子环境应该能看到父环境的变量
	result := child.LookupVariable("key")
	assert.Equal(t, "parent_value", result)
}

// TestBindingEnv_AddBinding 测试添加绑定
func TestBindingEnv_AddBinding(t *testing.T) {
	env := NewBindingEnv(nil)
	env.AddBinding("cc", "gcc")
	env.AddBinding("cflags", "-O2")

	assert.Len(t, env.vars, 2)
	assert.Equal(t, "gcc", env.vars["cc"])
	assert.Equal(t, "-O2", env.vars["cflags"])
}

// TestBindingEnv_LookupVariable 测试变量查找
func TestBindingEnv_LookupVariable(t *testing.T) {
	env := NewBindingEnv(nil)
	env.AddBinding("cc", "gcc")

	// 查找存在的变量
	result := env.LookupVariable("cc")
	assert.Equal(t, "gcc", result)

	// 查找不存在的变量
	result = env.LookupVariable("nonexistent")
	assert.Equal(t, "", result)
}

// TestBindingEnv_LookupVariable_Inheritance 测试变量继承
func TestBindingEnv_LookupVariable_Inheritance(t *testing.T) {
	parent := NewBindingEnv(nil)
	parent.AddBinding("cc", "gcc")

	child := NewBindingEnv(parent)
	child.AddBinding("cflags", "-O2")

	// 子环境应该能查找自己的变量
	result := child.LookupVariable("cflags")
	assert.Equal(t, "-O2", result)

	// 子环境应该能查找父环境的变量
	result = child.LookupVariable("cc")
	assert.Equal(t, "gcc", result)

	// 父环境不应该能看到子环境的变量
	result = parent.LookupVariable("cflags")
	assert.Equal(t, "", result)
}

// TestBindingEnv_LookupVariable_Override 测试变量覆盖
func TestBindingEnv_LookupVariable_Override(t *testing.T) {
	parent := NewBindingEnv(nil)
	parent.AddBinding("cc", "gcc")

	child := NewBindingEnv(parent)
	child.AddBinding("cc", "clang")

	// 子环境的变量应该覆盖父环境的变量
	result := child.LookupVariable("cc")
	assert.Equal(t, "clang", result)

	// 父环境的变量应该保持不变
	result = parent.LookupVariable("cc")
	assert.Equal(t, "gcc", result)
}

// TestBindingEnv_AddRule 测试添加规则
func TestBindingEnv_AddRule(t *testing.T) {
	env := NewBindingEnv(nil)
	rule := NewRule("cc")

	env.AddRule(rule)

	assert.Len(t, env.rules, 1)
	assert.Equal(t, rule, env.rules["cc"])
}

// TestBindingEnv_AddRule_Duplicate 测试重复规则
func TestBindingEnv_AddRule_Duplicate(t *testing.T) {
	env := NewBindingEnv(nil)
	rule1 := NewRule("cc")
	rule2 := NewRule("cc")

	env.AddRule(rule1)
	env.AddRule(rule2)

	// 第二个规则不应该覆盖第一个
	assert.Len(t, env.rules, 1)
	assert.Equal(t, rule1, env.rules["cc"])
}

// TestBindingEnv_LookupRule 测试规则查找
func TestBindingEnv_LookupRule(t *testing.T) {
	env := NewBindingEnv(nil)
	rule := NewRule("cc")
	env.AddRule(rule)

	// 查找存在的规则
	result := env.LookupRule("cc")
	assert.Equal(t, rule, result)

	// 查找不存在的规则
	result = env.LookupRule("nonexistent")
	assert.Nil(t, result)
}

// TestBindingEnv_LookupRule_Inheritance 测试规则继承
func TestBindingEnv_LookupRule_Inheritance(t *testing.T) {
	parent := NewBindingEnv(nil)
	parentRule := NewRule("cc")
	parent.AddRule(parentRule)

	child := NewBindingEnv(parent)
	childRule := NewRule("link")
	child.AddRule(childRule)

	// 子环境应该能查找自己的规则
	result := child.LookupRule("link")
	assert.Equal(t, childRule, result)

	// 子环境应该能查找父环境的规则
	result = child.LookupRule("cc")
	assert.Equal(t, parentRule, result)

	// 父环境不应该能看到子环境的规则
	result = parent.LookupRule("link")
	assert.Nil(t, result)
}

// TestBindingEnv_LookupRuleCurrentScope 测试当前作用域规则查找
func TestBindingEnv_LookupRuleCurrentScope(t *testing.T) {
	parent := NewBindingEnv(nil)
	parentRule := NewRule("cc")
	parent.AddRule(parentRule)

	child := NewBindingEnv(parent)

	// 当前作用域查找不应该搜索父环境
	result := child.LookupRuleCurrentScope("cc")
	assert.Nil(t, result)

	// 在当前作用域添加规则
	childRule := NewRule("cc")
	child.AddRule(childRule)
	result = child.LookupRuleCurrentScope("cc")
	assert.Equal(t, childRule, result)
}

// TestBindingEnv_GetRules 测试获取所有规则
func TestBindingEnv_GetRules(t *testing.T) {
	env := NewBindingEnv(nil)
	rule1 := NewRule("cc")
	rule2 := NewRule("link")

	env.AddRule(rule1)
	env.AddRule(rule2)

	rules := env.GetRules()
	assert.Len(t, rules, 2)
	assert.Equal(t, rule1, rules["cc"])
	assert.Equal(t, rule2, rules["link"])
}

// TestBindingEnv_LookupWithFallback 测试带回退的查找
func TestBindingEnv_LookupWithFallback(t *testing.T) {
	env := NewBindingEnv(nil)
	env.AddBinding("key1", "direct_value")

	// 直接找到变量
	result := env.LookupWithFallback("key1", nil, nil)
	assert.Equal(t, "direct_value", result)

	// 通过 EvalString 回退
	eval := &EvalString{}
	eval.AddText("fallback_value")
	result = env.LookupWithFallback("key2", eval, nil)
	assert.Equal(t, "fallback_value", result)

	// 父环境回退
	parent := NewBindingEnv(nil)
	parent.AddBinding("key3", "parent_value")
	child := NewBindingEnv(parent)
	result = child.LookupWithFallback("key3", nil, nil)
	assert.Equal(t, "parent_value", result)

	// 最终回退到空字符串
	result = env.LookupWithFallback("nonexistent", nil, nil)
	assert.Equal(t, "", result)
}

// TestBindingEnv_LookupWithFallback_EvalWithEnv 测试带环境的 EvalString 回退
func TestBindingEnv_LookupWithFallback_EvalWithEnv(t *testing.T) {
	env := NewBindingEnv(nil)
	env.AddBinding("in", "foo.c")

	eval := &EvalString{}
	eval.AddSpecial("in")

	result := env.LookupWithFallback("command", eval, env)
	assert.Equal(t, "foo.c", result)
}

// TestBindingEnv_NestedScopes 测试嵌套作用域
func TestBindingEnv_NestedScopes(t *testing.T) {
	grandparent := NewBindingEnv(nil)
	grandparent.AddBinding("a", "grandparent_a")

	parent := NewBindingEnv(grandparent)
	parent.AddBinding("b", "parent_b")

	child := NewBindingEnv(parent)
	child.AddBinding("c", "child_c")

	// 应该能访问所有作用域
	assert.Equal(t, "grandparent_a", child.LookupVariable("a"))
	assert.Equal(t, "parent_b", child.LookupVariable("b"))
	assert.Equal(t, "child_c", child.LookupVariable("c"))

	// parent 不能访问 child
	assert.Equal(t, "", parent.LookupVariable("c"))

	// grandparent 只能访问自己的
	assert.Equal(t, "", grandparent.LookupVariable("b"))
	assert.Equal(t, "", grandparent.LookupVariable("c"))
}

// TestBindingEnv_EmptyVariable 测试空变量
func TestBindingEnv_EmptyVariable(t *testing.T) {
	env := NewBindingEnv(nil)
	env.AddBinding("empty", "")

	result := env.LookupVariable("empty")
	assert.Equal(t, "", result)
}

// TestBindingEnv_RuleShadowing 测试规则遮蔽
func TestBindingEnv_RuleShadowing(t *testing.T) {
	parent := NewBindingEnv(nil)
	parentRule := NewRule("cc")
	parentRule.AddBinding("command", &EvalString{singleToken: "gcc"})
	parent.AddRule(parentRule)

	child := NewBindingEnv(parent)
	childRule := NewRule("cc")
	childRule.AddBinding("command", &EvalString{singleToken: "clang"})
	child.AddRule(childRule)

	// 子环境应该看到自己的规则
	result := child.LookupRule("cc")
	assert.Equal(t, childRule, result)

	// 子环境当前作用域也应该是自己的规则
	result = child.LookupRuleCurrentScope("cc")
	assert.Equal(t, childRule, result)
}
