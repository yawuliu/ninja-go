package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewState 测试 State 初始化
func TestNewState(t *testing.T) {
	state := NewState()
	require.NotNil(t, state)

	// 检查初始状态
	assert.NotNil(t, state.paths_)
	assert.NotNil(t, state.pools_)
	assert.NotNil(t, state.Rules)
	assert.NotNil(t, state.edges_)
	assert.NotNil(t, state.bindings_)
	assert.NotNil(t, state.defaults_)

	// 检查默认池
	assert.NotNil(t, state.LookupPool(""))
	assert.NotNil(t, state.LookupPool("console"))

	// 检查 phony 规则
	phony := state.bindings_.LookupRule("phony")
	assert.NotNil(t, phony)
}

// TestState_AddNode 测试添加节点
func TestState_AddNode(t *testing.T) {
	state := NewState()

	// 添加新节点
	node1 := state.GetNode("foo.txt", 0)
	require.NotNil(t, node1)
	assert.Equal(t, "foo.txt", node1.path_)
	assert.Equal(t, -1, node1.id_)

	// 添加另一个节点
	node2 := state.GetNode("bar.txt", 0)
	require.NotNil(t, node2)
	assert.Equal(t, "bar.txt", node2.path_)
	assert.Equal(t, -1, node2.id_)

	// 重复添加应该返回已有节点
	node3 := state.GetNode("foo.txt", 0)
	assert.Equal(t, node1, node3)
}

// TestState_LookupNode 测试查找节点
func TestState_LookupNode(t *testing.T) {
	state := NewState()

	// 查找不存在的节点
	node := state.LookupNode("nonexistent.txt")
	assert.Nil(t, node)

	// 添加后查找
	state.GetNode("exists.txt", 0)
	node = state.LookupNode("exists.txt")
	assert.NotNil(t, node)
	assert.Equal(t, "exists.txt", node.path_)
}

// TestState_GetNodeByID 测试通过 id_ 获取节点
func TestState_GetNodeByID(t *testing.T) {
	state := NewState()

	node1 := state.GetNode("foo.txt", 0)
	node2 := state.GetNode("bar.txt", 0)

	// 通过 id_ 获取
	found1 := state.LookupNode("foo.txt")
	assert.Equal(t, node1, found1)

	found2 := state.LookupNode("bar.txt")
	assert.Equal(t, node2, found2)
}

// TestState_AddEdge 测试添加边
func TestState_AddEdge(t *testing.T) {
	state := NewState()
	rule := &Rule{Name: "cc"}

	edge := state.AddEdge(rule)
	require.NotNil(t, edge)
	assert.Equal(t, rule, edge.rule_)
	assert.Equal(t, uint64(0), edge.id_)
	assert.Equal(t, kDefaultPool, edge.pool_)
	assert.Equal(t, state.bindings_, edge.env_)

	// 添加第二条边，id_ 应该递增
	edge2 := state.AddEdge(rule)
	assert.Equal(t, uint64(1), edge2.id_)
}

// TestState_AddIn 测试添加输入
func TestState_AddIn(t *testing.T) {
	state := NewState()
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)

	state.AddIn(edge, "input.c", 0)

	require.Len(t, edge.inputs_, 1)
	assert.Equal(t, "input.c", edge.inputs_[0].path_)

	// 检查节点的出边
	node := state.LookupNode("input.c")
	require.NotNil(t, node)
	assert.Contains(t, node.out_edges_, edge)
}

// TestState_AddOut 测试添加输出
func TestState_AddOut(t *testing.T) {
	state := NewState()
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	var err string
	state.AddOut(edge, "output.o", 0, &err)
	require.Equal(t, err, "")

	require.Len(t, edge.outputs_, 1)
	assert.Equal(t, "output.o", edge.outputs_[0].path_)

	// 检查节点的入边
	node := state.LookupNode("output.o")
	require.NotNil(t, node)
	assert.Equal(t, edge, node.in_edge())
}

// TestState_AddOut_Duplicate 测试重复输出
func TestState_AddOut_Duplicate(t *testing.T) {
	state := NewState()
	rule := &Rule{Name: "cc"}
	edge1 := state.AddEdge(rule)
	edge2 := state.AddEdge(rule)

	// 添加第一个输出
	var err string
	state.AddOut(edge1, "output.o", 0, &err)
	require.Equal(t, err, "")

	// 同一边的重复输出应该报错
	state.AddOut(edge1, "output.o", 0, &err)
	assert.NotNil(t, err)
	assert.Contains(t, err, "defined as an output multiple times")

	// 不同边的相同输出应该报错
	state.AddOut(edge2, "output.o", 0, &err)
	assert.NotNil(t, err)
	assert.Contains(t, err, "multiple rules generate")
}

// TestState_AddDefault 测试添加默认目标
func TestState_AddDefault(t *testing.T) {
	state := NewState()

	// 先添加一个节点
	state.GetNode("target", 0)
	rule := &Rule{Name: "phony"}
	edge := state.AddEdge(rule)
	var err string
	state.AddOut(edge, "target", 0, &err)

	// 添加为默认目标
	state.AddDefault("target", &err)
	require.Equal(t, err, "")
	assert.Len(t, state.defaults_, 1)

	// 不存在的节点应该报错
	state.AddDefault("nonexistent", &err)
	assert.NotNil(t, err)
}

// TestState_Reset 测试重置状态
func TestState_Reset(t *testing.T) {
	state := NewState()

	// 创建一些节点和边
	node := state.GetNode("foo.txt", 0)
	node.dirty_ = true
	node.mtime_ = 12345
	node.exists_ = 1

	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	edge.outputs_ready_ = true
	edge.deps_loaded_ = true
	edge.mark_ = VisitDone

	// 重置
	state.Reset()

	// 验证状态被重置
	assert.False(t, node.dirty_)
	assert.Equal(t, int64(-1), node.mtime_)
	assert.Equal(t, false, node.Exists())

	assert.False(t, edge.outputs_ready_)
	assert.False(t, edge.deps_loaded_)
	assert.Equal(t, VisitNone, edge.mark_)
}

// TestState_RootNodes 测试根节点查找
func TestState_RootNodes(t *testing.T) {
	state := NewState()
	rule := &Rule{Name: "cc"}

	// 空状态
	var err string
	roots := state.RootNodes(&err)
	require.Equal(t, err, "")
	assert.Empty(t, roots)

	// 创建简单图: a -> b -> c
	// c 应该是根节点（没有其他边依赖它）
	edge1 := state.AddEdge(rule)

	state.AddIn(edge1, "a", 0)
	state.AddOut(edge1, "b", 0, &err)

	edge2 := state.AddEdge(rule)
	state.AddIn(edge2, "b", 0)
	state.AddOut(edge2, "c", 0, &err)

	roots = state.RootNodes(&err)
	require.Equal(t, err, "")
	require.Len(t, roots, 1)
	assert.Equal(t, "c", roots[0].path_)
}

// TestState_RootNodes_Cycle 测试循环依赖的根节点
func TestState_RootNodes_Cycle(t *testing.T) {
	state := NewState()
	rule := &Rule{Name: "cc"}

	// 创建循环: a -> b, b -> a
	edge1 := state.AddEdge(rule)
	var err string
	state.AddIn(edge1, "a", 0)
	state.AddOut(edge1, "b", 0, &err)

	edge2 := state.AddEdge(rule)
	state.AddIn(edge2, "b", 0)
	state.AddOut(edge2, "a", 0, &err)

	// 这种情况下没有根节点
	roots := state.RootNodes(&err)
	assert.NotNil(t, err)
	assert.Contains(t, err, "could not determine root nodes")
	assert.Empty(t, roots)
}

// TestState_DefaultNodes 测试默认节点
func TestState_DefaultNodes(t *testing.T) {
	state := NewState()
	rule := &Rule{Name: "cc"}

	// 没有默认目标时应该返回根节点
	var err string
	edge := state.AddEdge(rule)
	state.AddIn(edge, "input", 0)
	state.AddOut(edge, "output", 0, &err)

	defaults := state.DefaultNodes(&err)
	require.Len(t, defaults, 1)
	assert.Equal(t, "output", defaults[0].path_)

	// 设置默认目标后应该返回默认目标
	state.GetNode("default_target", 0)
	edge2 := state.AddEdge(rule)

	state.AddOut(edge2, "default_target", 0, &err)
	state.AddDefault("default_target", &err)

	defaults = state.DefaultNodes(&err)
	require.Len(t, defaults, 1)
	assert.Equal(t, "default_target", defaults[0].path_)
}

// TestNode_NewNode 测试节点创建
func TestNode_NewNode(t *testing.T) {
	node := NewNode("test.txt", 0x1234)
	require.NotNil(t, node)

	assert.Equal(t, "test.txt", node.path_)
	assert.Equal(t, uint64(0x1234), node.slash_bits_)
	assert.Equal(t, int64(-1), node.mtime_)
	assert.Equal(t, false, node.Exists())
	assert.True(t, node.generated_by_dep_loader_)
	assert.Equal(t, -1, node.id_)
}

// TestNode_ResetState 测试节点状态重置
func TestNode_ResetState(t *testing.T) {
	node := NewNode("test.txt", 0)
	node.mtime_ = 12345
	node.exists_ = 1
	node.dirty_ = true

	node.ResetState()

	assert.Equal(t, int64(-1), node.mtime_)
	assert.Equal(t, false, node.Exists())
	assert.False(t, node.dirty_)
}

// TestNode_MarkMissing 测试标记为缺失
func TestNode_MarkMissing(t *testing.T) {
	node := NewNode("test.txt", 0)

	node.MarkMissing()

	assert.Equal(t, int64(0), node.mtime_)
	assert.Equal(t, false, node.Exists())
}

// TestNode_AddOutEdge 测试添加出边
func TestNode_AddOutEdge(t *testing.T) {
	node := NewNode("test.txt", 0)
	rule := &Rule{Name: "cc"}
	edge1 := &Edge{rule_: rule}
	edge2 := &Edge{rule_: rule}

	// 添加第一条边
	node.AddOutEdge(edge1)
	assert.Len(t, node.out_edges_, 1)

	// 添加第二条边
	node.AddOutEdge(edge2)
	assert.Len(t, node.out_edges_, 2)

	// 重复添加应该被忽略
	node.AddOutEdge(edge1)
	assert.Len(t, node.out_edges_, 2)
}

// TestNode_AddValidationOutEdge 测试添加验证出边
func TestNode_AddValidationOutEdge(t *testing.T) {
	node := NewNode("test.txt", 0)
	rule := &Rule{Name: "cc"}
	edge := &Edge{rule_: rule}

	node.AddValidationOutEdge(edge)
	assert.Len(t, node.validation_out_edges_, 1)
}

// TestNode_IsExists 测试存在性检查
func TestNode_IsExists(t *testing.T) {
	node := NewNode("test.txt", 0)

	// 初始状态
	assert.False(t, node.IsExists())

	// 标记为不存在
	node.exists_ = ExistenceStatusMissing
	assert.False(t, node.IsExists())

	// 标记为存在
	node.exists_ = ExistenceStatusExists
	assert.True(t, node.IsExists())
}

// TestNode_UpdatePhonyMtime 测试 Phony 节点时间更新
func TestNode_UpdatePhonyMtime(t *testing.T) {
	node := NewNode("phony_target", 0)

	// 不存在的节点应该更新时间
	node.UpdatePhonyMtime(1000)
	assert.Equal(t, int64(1000), node.mtime_)

	node.UpdatePhonyMtime(500)
	assert.Equal(t, int64(1000), node.mtime_) // 应该保持较大值

	node.UpdatePhonyMtime(2000)
	assert.Equal(t, int64(2000), node.mtime_)
}

// TestEdge_EvaluateCommand 测试命令求值
func TestEdge_EvaluateCommand(t *testing.T) {
	rule := &Rule{
		Name: "cc",
	}
	edge := &Edge{
		rule_: rule,
		inputs_: []*Node{
			{path_: "foo.c"},
		},
		outputs_: []*Node{
			{path_: "foo.o"},
		},
	}

	cmd := edge.EvaluateCommand(false)
	assert.Equal(t, "gcc foo.c -o foo.o", cmd)
}

// TestEdge_GetBinding 测试变量绑定
func TestEdge_GetBinding(t *testing.T) {
	rule := &Rule{
		Name: "cc",
	}
	edge := &Edge{
		rule_: rule,
	}

	// 获取 rule 的变量
	cmd := edge.GetBinding("command")
	assert.Equal(t, "gcc -c", cmd)

	// 不存在的变量
	missing := edge.GetBinding("nonexistent")
	assert.Equal(t, "", missing)
}

// TestEdge_IsImplicit 测试隐式依赖检查
func TestEdge_IsImplicit(t *testing.T) {
	edge := &Edge{
		inputs_:          make([]*Node, 5),
		implicit_deps_:   2,
		order_only_deps_: 1,
	}

	// 前 2 个是普通依赖
	assert.False(t, edge.IsImplicit(0))
	assert.False(t, edge.IsImplicit(1))

	// 接下来 2 个是隐式依赖
	assert.True(t, edge.IsImplicit(2))
	assert.True(t, edge.IsImplicit(3))

	// 最后 1 个是 order-only 依赖
	assert.False(t, edge.IsImplicit(4))
}

// TestEdge_IsOrderOnly 测试 order-only 依赖检查
func TestEdge_IsOrderOnly(t *testing.T) {
	edge := &Edge{
		inputs_:          make([]*Node, 5),
		implicit_deps_:   2,
		order_only_deps_: 1,
	}

	// 前 4 个不是 order-only
	assert.False(t, edge.is_order_only(0))
	assert.False(t, edge.is_order_only(3))

	// 最后 1 个是 order-only
	assert.True(t, edge.is_order_only(4))
}

// TestEdge_IsImplicitOut 测试隐式输出检查
func TestEdge_IsImplicitOut(t *testing.T) {
	edge := &Edge{
		outputs_:       make([]*Node, 3),
		implicit_outs_: 1,
	}

	// 前 2 个是普通输出
	assert.False(t, edge.IsImplicitOut(0))
	assert.False(t, edge.IsImplicitOut(1))

	// 最后 1 个是隐式输出
	assert.True(t, edge.IsImplicitOut(2))
}

// TestEdge_IsPhony 测试 Phony 规则检查
func TestEdge_IsPhony(t *testing.T) {
	// Phony 规则
	phonyRule := &Rule{Name: "phony"}
	phonyEdge := &Edge{rule_: phonyRule}
	assert.True(t, phonyEdge.IsPhony())

	// 普通规则
	ccRule := &Rule{Name: "cc"}
	ccEdge := &Edge{rule_: ccRule}
	assert.False(t, ccEdge.IsPhony())

	// 无规则
	noRuleEdge := &Edge{}
	assert.False(t, noRuleEdge.IsPhony())
}

// TestEdge_UseConsole 测试控制台池检查
func TestEdge_UseConsole(t *testing.T) {
	// Console 池
	consoleEdge := &Edge{pool_: kConsolePool}
	assert.True(t, consoleEdge.use_console())

	// 默认池
	defaultEdge := &Edge{pool_: kDefaultPool}
	assert.False(t, defaultEdge.use_console())

	// 无池
	noPoolEdge := &Edge{}
	assert.False(t, noPoolEdge.use_console())
}

// TestEdge_AllInputsReady 测试输入就绪检查
func TestEdge_AllInputsReady(t *testing.T) {
	rule := &Rule{Name: "cc"}

	// 创建输入节点和边
	in1 := &Node{path_: "in1"}
	in1Edge := &Edge{rule_: rule, outputs_ready_: true}
	in1.in_edge_ = in1Edge

	in2 := &Node{path_: "in2"}
	// in2 没有入边（源文件）

	edge := &Edge{
		rule_:   rule,
		inputs_: []*Node{in1, in2},
	}

	// 所有输入就绪
	assert.True(t, edge.AllInputsReady())

	// 让一个输入不就绪
	in1Edge.outputs_ready_ = false
	assert.False(t, edge.AllInputsReady())
}

// TestEdge_MaybePhonyCycleDiagnostic 测试 Phony 循环诊断
func TestEdge_MaybePhonyCycleDiagnostic(t *testing.T) {
	// 符合诊断条件的边
	phonyRule := &Rule{Name: "phony"}
	out := &Node{path_: "out"}
	edge := &Edge{
		rule_:          phonyRule,
		outputs_:       []*Node{out},
		implicit_outs_: 0,
		implicit_deps_: 0,
	}
	assert.True(t, edge.MaybePhonyCycleDiagnostic())

	// 非 phony 规则
	ccRule := &Rule{Name: "cc"}
	edge2 := &Edge{rule_: ccRule, outputs_: []*Node{out}}
	assert.False(t, edge2.MaybePhonyCycleDiagnostic())

	// 多个输出
	edge3 := &Edge{
		rule_:    phonyRule,
		outputs_: []*Node{{path_: "out1"}, {path_: "out2"}},
	}
	assert.False(t, edge3.MaybePhonyCycleDiagnostic())
}
