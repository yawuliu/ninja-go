package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPlan 测试 Plan 初始化
func TestNewPlan(t *testing.T) {
	plan := NewPlan(nil)
	require.NotNil(t, plan)

	assert.NotNil(t, plan.want_)
	assert.NotNil(t, plan.ready_)
	assert.Empty(t, plan.targets_)
	assert.Equal(t, 0, plan.command_edges_)
	assert.Equal(t, 0, plan.wanted_edges_)
}

// TestPlan_Reset 测试 Plan 重置
func TestPlan_Reset(t *testing.T) {
	plan := NewPlan(nil)

	// 添加一些状态
	edge := &Edge{rule_: &Rule{Name: "cc"}}
	plan.want_[edge] = kWantToStart
	plan.targets_ = append(plan.targets_, &Node{path_: "target"})
	plan.command_edges_ = 5
	plan.wanted_edges_ = 10

	// 重置
	plan.Reset()

	// 验证状态被重置
	assert.Empty(t, plan.want_)
	assert.Empty(t, plan.targets_)
	assert.Equal(t, 0, plan.command_edges_)
	assert.Equal(t, 0, plan.wanted_edges_)
}

// TestPlan_AddTarget 测试添加目标
func TestPlan_AddTarget(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建简单图: in -> out
	rule := NewRule("cc")
	cmdEval := &EvalString{}
	cmdEval.AddText("gcc $in -o $out")
	rule.AddBinding("command", cmdEval)
	edge := state.AddEdge(rule)
	inNode := state.GetNode("in.c", 0)
	outNode := state.GetNode("out.o", 0)
	edge.inputs_ = []*Node{inNode}
	edge.outputs_ = []*Node{outNode}
	var err string
	state.AddIn(edge, "in.c", 0)
	state.AddOut(edge, "out.o", 0, &err)

	// 标记 out 为 dirty
	outNode.dirty_ = true

	// 添加目标
	ok := plan.AddTarget(outNode, &err)
	assert.Equal(t, err, "")
	assert.True(t, ok)

	// 验证目标被添加
	assert.Len(t, plan.targets_, 1)
	assert.Equal(t, outNode, plan.targets_[0])

	// 验证边被标记为 wanted
	assert.Equal(t, kWantToStart, plan.want_[edge])
}

// TestPlan_AddTarget_MissingInput 测试缺失输入
func TestPlan_AddTarget_MissingInput(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建图，输入缺失
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	inNode := state.GetNode("missing.c", 0)
	outNode := state.GetNode("out.o", 0)
	// 使用 AddIn 和 AddOut 来正确设置连接关系
	var err string
	state.AddIn(edge, "missing.c", 0)
	state.AddOut(edge, "out.o", 0, &err)

	// 标记输入为 dirty 且没有生成边（源文件）
	inNode.dirty_ = true
	inNode.generated_by_dep_loader_ = false
	// 标记输出为 dirty 以触发构建检查
	outNode.dirty_ = true

	// 添加目标应该失败
	ok := plan.AddTarget(outNode, &err)
	assert.Equal(t, err, "")
	assert.Contains(t, err, "missing and no known rule")
	assert.False(t, ok)
}

// TestPlan_AddTarget_AlreadyUpToDate 测试已是最新
func TestPlan_AddTarget_AlreadyUpToDate(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建图
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	outNode := state.GetNode("out.o", 0)
	edge.outputs_ = []*Node{outNode}

	// 标记输出已就绪
	edge.outputs_ready_ = true

	// 添加目标
	var err string
	ok := plan.AddTarget(outNode, &err)
	require.Equal(t, err, "")
	assert.False(t, ok) // 不需要构建
}

// TestPlan_edgeWanted 测试边计数
func TestPlan_edgeWanted(t *testing.T) {
	plan := NewPlan(nil)

	// 普通边
	edge := &Edge{rule_: &Rule{Name: "cc"}}
	plan.edgeWanted(edge)
	assert.Equal(t, 1, plan.wanted_edges_)
	assert.Equal(t, 1, plan.command_edges_)

	// Phony 边
	phonyEdge := &Edge{rule_: &Rule{Name: "phony"}}
	plan.edgeWanted(phonyEdge)
	assert.Equal(t, 2, plan.wanted_edges_)
	assert.Equal(t, 1, plan.command_edges_) // phony 不增加 command_edges_
}

// TestPlan_FindWork 测试查找工作
func TestPlan_FindWork(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 空队列
	work := plan.FindWork()
	assert.Nil(t, work)

	// 添加边到队列
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	edge.inputs_ = []*Node{} // 无输入，立即可用
	plan.want_[edge] = kWantToFinish

	// 添加到优先队列
	plan.ready_.Push(edge)

	// 查找工作
	work = plan.FindWork()
	assert.Equal(t, edge, work)
	assert.Equal(t, 0, plan.ready_.Len())
}

// TestPlan_EdgeFinished 测试边完成
func TestPlan_EdgeFinished(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建图: in -> out
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	outNode := state.GetNode("out.o", 0)
	edge.outputs_ = []*Node{outNode}

	// 标记为 wanted
	plan.want_[edge] = kWantToFinish
	plan.wanted_edges_ = 1
	plan.command_edges_ = 1

	// 完成边
	var err string
	plan.EdgeFinished(edge, kEdgeSucceeded, &err)
	require.Equal(t, err, "")

	// 验证状态
	assert.Equal(t, 0, plan.wanted_edges_)
	assert.NotContains(t, plan.want_, edge)
	assert.True(t, edge.outputs_ready_)
}

// TestPlan_EdgeFinished_Failed 测试边失败
func TestPlan_EdgeFinished_Failed(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	outNode := state.GetNode("out.o", 0)
	edge.outputs_ = []*Node{outNode}

	plan.want_[edge] = kWantToFinish

	// 边失败
	var err string
	plan.EdgeFinished(edge, kEdgeFailed, &err)
	require.Equal(t, err, "")

	// 输出不应标记为就绪
	assert.False(t, edge.outputs_ready_)
}

// TestPlan_scheduleWork 测试工作调度
func TestPlan_scheduleWork(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 使用默认池（深度无限）
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	edge.pool_ = kDefaultPool

	// 标记为想要开始
	plan.want_[edge] = kWantToStart

	// 调度工作
	plan.ScheduleWork(plan.want_, edge, kWantToStart)

	// 应该被标记为想要完成，并加入队列
	assert.Equal(t, kWantToFinish, plan.want_[edge])
	assert.Equal(t, 1, plan.ready_.Len())
}

// TestPlan_computeCriticalPath 测试关键路径计算
func TestPlan_computeCriticalPath(t *testing.T) {
	state := NewState()
	plan := NewPlan(&Builder{state_: state})

	// 创建链式图: a -> b -> c
	rule := &Rule{Name: "cc"}

	edge1 := state.AddEdge(rule)
	aNode := state.GetNode("a.o", 0)
	bNode := state.GetNode("b.o", 0)
	edge1.inputs_ = []*Node{aNode}
	edge1.outputs_ = []*Node{bNode}

	edge2 := state.AddEdge(rule)
	cNode := state.GetNode("c.o", 0)
	edge2.inputs_ = []*Node{bNode}
	edge2.outputs_ = []*Node{cNode}

	// 添加目标
	cNode.dirty_ = true
	var err string
	plan.AddTarget(cNode, &err)

	// 计算关键路径
	plan.computeCriticalPath()

	// c 是最终目标，权重应该最高
	// edge1 (a->b) 应该在关键路径上
	// edge2 (b->c) 依赖于 edge1
	assert.Greater(t, edge2.critical_path_weight_, edge1.critical_path_weight_)
}

// TestPlan_MoreToDo 测试更多工作检查
func TestPlan_MoreToDo(t *testing.T) {
	plan := NewPlan(nil)

	// 初始状态
	assert.False(t, plan.MoreToDo())

	// 添加 wanted 边
	plan.wanted_edges_ = 1
	plan.command_edges_ = 1

	assert.True(t, plan.MoreToDo())

	// 没有 command 边了
	plan.command_edges_ = 0
	assert.False(t, plan.MoreToDo())
}

// TestPlan_CommandEdgeCount 测试命令边计数
func TestPlan_CommandEdgeCount(t *testing.T) {
	plan := NewPlan(nil)
	assert.Equal(t, 0, plan.CommandEdgeCount())

	plan.command_edges_ = 5
	assert.Equal(t, 5, plan.CommandEdgeCount())
}

// TestPlan_CleanNode 测试节点清理
func TestPlan_CleanNode(t *testing.T) {
	state := NewState()

	// 创建简单图
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	inNode := state.GetNode("in.c", 0)
	outNode := state.GetNode("out.o", 0)
	edge.inputs_ = []*Node{inNode}
	edge.outputs_ = []*Node{outNode}

	// 创建 mock scanner
	scanner := &DependencyScan{}

	// 创建 plan_
	plan := NewPlan(nil)
	plan.want_[edge] = kWantToFinish

	// 清理节点
	inNode.dirty_ = true
	outNode.dirty_ = true

	var err string
	plan.CleanNode(scanner, inNode, &err)
	require.Equal(t, err, "")

	// 输入节点应该被标记为 clean
	assert.False(t, inNode.dirty_)
}

// TestPlan_DyndepsLoaded 测试动态依赖加载
func TestPlan_DyndepsLoaded(t *testing.T) {
	state := NewState()
	plan := NewPlan(&Builder{state_: state})

	// 创建基础图
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	outNode := state.GetNode("out.o", 0)
	edge.outputs_ = []*Node{outNode}

	// 创建 mock dyndep 文件
	ddf := DyndepFile{}

	// 创建 mock scanner
	scanner := &DependencyScan{}

	// 调用 DyndepsLoaded
	var err string
	plan.DyndepsLoaded(scanner, outNode, ddf, &err)
	require.Equal(t, err, "")
}

// TestPlan_RefreshDyndepDependents 测试刷新动态依赖
func TestPlan_RefreshDyndepDependents(t *testing.T) {
	state := NewState()
	plan := NewPlan(&Builder{state_: state})

	// 创建节点
	node := state.GetNode("test.o", 0)

	// 创建 mock scanner
	scanner := &DependencyScan{}

	// 调用 RefreshDyndepDependents
	var err string
	plan.RefreshDyndepDependents(scanner, node, &err)
	require.Equal(t, err, "")
}

// TestPlan_nodeFinished 测试节点完成
func TestPlan_nodeFinished(t *testing.T) {
	state := NewState()
	plan := NewPlan(&Builder{state_: state})

	// 创建图
	rule := &Rule{Name: "cc"}
	edge1 := state.AddEdge(rule)
	inNode := state.GetNode("in.c", 0)
	midNode := state.GetNode("mid.o", 0)
	edge1.inputs_ = []*Node{inNode}
	edge1.outputs_ = []*Node{midNode}

	edge2 := state.AddEdge(rule)
	outNode := state.GetNode("out.o", 0)
	edge2.inputs_ = []*Node{midNode}
	edge2.outputs_ = []*Node{outNode}

	// 设置计划
	plan.want_[edge1] = kWantToStart
	plan.want_[edge2] = kWantToStart

	// 完成中间节点
	var err string
	plan.nodeFinished(midNode, &err)
	require.Equal(t, err, "")
}

// TestPlan_edgeMaybeReady 测试边可能就绪
func TestPlan_edgeMaybeReady(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建边，所有输入就绪
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	inNode := state.GetNode("in.c", 0)
	outNode := state.GetNode("out.o", 0)
	edge.inputs_ = []*Node{inNode}
	edge.outputs_ = []*Node{outNode}

	// 输入就绪
	plan.want_[edge] = kWantToStart

	// 检查边是否就绪
	var err string
	plan.EdgeMaybeReady(plan.want_, edge, kWantToStart, &err)
	require.Equal(t, err, "")
}

// TestPlan_scheduleInitialEdges 测试初始边调度
func TestPlan_scheduleInitialEdges(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建立即可用的边
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	outNode := state.GetNode("out.o", 0)
	edge.outputs_ = []*Node{outNode}
	edge.inputs_ = []*Node{} // 无输入，立即可用

	plan.want_[edge] = kWantToStart

	// 调度初始边
	plan.ScheduleInitialEdges()

	// 验证边被调度
	assert.Equal(t, 1, plan.ready_.Len())
}

// TestPlan_AddTarget_Recursive 测试递归添加目标
func TestPlan_AddTarget_Recursive(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建多层依赖: a -> b -> c
	rule := &Rule{Name: "cc"}

	edge1 := state.AddEdge(rule)
	cNode := state.GetNode("c.c", 0)
	bNode := state.GetNode("b.o", 0)
	edge1.inputs_ = []*Node{cNode}
	edge1.outputs_ = []*Node{bNode}

	edge2 := state.AddEdge(rule)
	aNode := state.GetNode("a.o", 0)
	edge2.inputs_ = []*Node{bNode}
	edge2.outputs_ = []*Node{aNode}

	// 标记为 dirty
	aNode.dirty_ = true
	bNode.dirty_ = true

	// 添加最终目标
	var err string
	ok := plan.AddTarget(aNode, &err)
	require.Equal(t, err, "")
	assert.True(t, ok)

	// 验证所有边都被标记
	assert.Contains(t, plan.want_, edge1)
	assert.Contains(t, plan.want_, edge2)
}

// TestPlan_unmarkDependents 测试取消标记依赖
func TestPlan_unmarkDependents(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建图
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	inNode := state.GetNode("in.c", 0)
	outNode := state.GetNode("out.o", 0)
	edge.inputs_ = []*Node{inNode}
	edge.outputs_ = []*Node{outNode}

	// 设置标记
	edge.mark_ = VisitDone
	plan.want_[edge] = kWantToStart

	// 取消标记
	dependents := make(map[*Node]bool)
	plan.unmarkDependents(inNode, dependents)

	// 验证标记被清除
	assert.Equal(t, VisitNone, edge.mark_)
}

// TestPlan_AddTarget_DyndepPending 测试动态依赖待处理
func TestPlan_AddTarget_DyndepPending(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建带 dyndep 的边
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	outNode := state.GetNode("out.o", 0)
	edge.outputs_ = []*Node{outNode}

	// 设置 dyndep 待处理（不影响基本测试）
	outNode.dyndep_pending_ = true
	outNode.dirty_ = true

	// 添加目标
	var err string
	ok := plan.AddTarget(outNode, &err)
	require.Equal(t, err, "")
	assert.True(t, ok)
}
