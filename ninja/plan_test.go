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

	assert.NotNil(t, plan.want)
	assert.NotNil(t, plan.ready)
	assert.Empty(t, plan.targets)
	assert.Equal(t, 0, plan.commandEdges)
	assert.Equal(t, 0, plan.wantedEdges)
}

// TestPlan_Reset 测试 Plan 重置
func TestPlan_Reset(t *testing.T) {
	plan := NewPlan(nil)

	// 添加一些状态
	edge := &Edge{Rule: &Rule{Name: "cc"}}
	plan.want[edge] = WantToStart
	plan.targets = append(plan.targets, &Node{path_: "target"})
	plan.commandEdges = 5
	plan.wantedEdges = 10

	// 重置
	plan.Reset()

	// 验证状态被重置
	assert.Empty(t, plan.want)
	assert.Empty(t, plan.targets)
	assert.Equal(t, 0, plan.commandEdges)
	assert.Equal(t, 0, plan.wantedEdges)
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
	inNode := state.AddNode("in.c", 0)
	outNode := state.AddNode("out.o", 0)
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
	assert.Len(t, plan.targets, 1)
	assert.Equal(t, outNode, plan.targets[0])

	// 验证边被标记为 wanted
	assert.Equal(t, WantToStart, plan.want[edge])
}

// TestPlan_AddTarget_MissingInput 测试缺失输入
func TestPlan_AddTarget_MissingInput(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建图，输入缺失
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	inNode := state.AddNode("missing.c", 0)
	outNode := state.AddNode("out.o", 0)
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
	outNode := state.AddNode("out.o", 0)
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
	edge := &Edge{Rule: &Rule{Name: "cc"}}
	plan.edgeWanted(edge)
	assert.Equal(t, 1, plan.wantedEdges)
	assert.Equal(t, 1, plan.commandEdges)

	// Phony 边
	phonyEdge := &Edge{Rule: &Rule{Name: "phony"}}
	plan.edgeWanted(phonyEdge)
	assert.Equal(t, 2, plan.wantedEdges)
	assert.Equal(t, 1, plan.commandEdges) // phony 不增加 commandEdges
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
	plan.want[edge] = WantToFinish

	// 添加到优先队列
	plan.ready.Push(edge)

	// 查找工作
	work = plan.FindWork()
	assert.Equal(t, edge, work)
	assert.Equal(t, 0, plan.ready.Len())
}

// TestPlan_EdgeFinished 测试边完成
func TestPlan_EdgeFinished(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建图: in -> out
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	outNode := state.AddNode("out.o", 0)
	edge.outputs_ = []*Node{outNode}

	// 标记为 wanted
	plan.want[edge] = WantToFinish
	plan.wantedEdges = 1
	plan.commandEdges = 1

	// 完成边
	var err string
	plan.EdgeFinished(edge, EdgeSucceeded, &err)
	require.Equal(t, err, "")

	// 验证状态
	assert.Equal(t, 0, plan.wantedEdges)
	assert.NotContains(t, plan.want, edge)
	assert.True(t, edge.outputs_ready_)
}

// TestPlan_EdgeFinished_Failed 测试边失败
func TestPlan_EdgeFinished_Failed(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	outNode := state.AddNode("out.o", 0)
	edge.outputs_ = []*Node{outNode}

	plan.want[edge] = WantToFinish

	// 边失败
	var err string
	plan.EdgeFinished(edge, EdgeFailed, &err)
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
	edge.Pool = DefaultPool

	// 标记为想要开始
	plan.want[edge] = WantToStart

	// 调度工作
	plan.scheduleWork(edge)

	// 应该被标记为想要完成，并加入队列
	assert.Equal(t, WantToFinish, plan.want[edge])
	assert.Equal(t, 1, plan.ready.Len())
}

// TestPlan_computeCriticalPath 测试关键路径计算
func TestPlan_computeCriticalPath(t *testing.T) {
	state := NewState()
	plan := NewPlan(&Builder{state: state})

	// 创建链式图: a -> b -> c
	rule := &Rule{Name: "cc"}

	edge1 := state.AddEdge(rule)
	aNode := state.AddNode("a.o", 0)
	bNode := state.AddNode("b.o", 0)
	edge1.inputs_ = []*Node{aNode}
	edge1.outputs_ = []*Node{bNode}

	edge2 := state.AddEdge(rule)
	cNode := state.AddNode("c.o", 0)
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
	plan.wantedEdges = 1
	plan.commandEdges = 1

	assert.True(t, plan.MoreToDo())

	// 没有 command 边了
	plan.commandEdges = 0
	assert.False(t, plan.MoreToDo())
}

// TestPlan_CommandEdgeCount 测试命令边计数
func TestPlan_CommandEdgeCount(t *testing.T) {
	plan := NewPlan(nil)
	assert.Equal(t, 0, plan.CommandEdgeCount())

	plan.commandEdges = 5
	assert.Equal(t, 5, plan.CommandEdgeCount())
}

// TestPlan_CleanNode 测试节点清理
func TestPlan_CleanNode(t *testing.T) {
	state := NewState()

	// 创建简单图
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	inNode := state.AddNode("in.c", 0)
	outNode := state.AddNode("out.o", 0)
	edge.inputs_ = []*Node{inNode}
	edge.outputs_ = []*Node{outNode}

	// 创建 mock scanner
	scanner := &DependencyScan{}

	// 创建 plan
	plan := NewPlan(nil)
	plan.want[edge] = WantToFinish

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
	plan := NewPlan(&Builder{state: state})

	// 创建基础图
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	outNode := state.AddNode("out.o", 0)
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
	plan := NewPlan(&Builder{state: state})

	// 创建节点
	node := state.AddNode("test.o", 0)

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
	plan := NewPlan(&Builder{state: state})

	// 创建图
	rule := &Rule{Name: "cc"}
	edge1 := state.AddEdge(rule)
	inNode := state.AddNode("in.c", 0)
	midNode := state.AddNode("mid.o", 0)
	edge1.inputs_ = []*Node{inNode}
	edge1.outputs_ = []*Node{midNode}

	edge2 := state.AddEdge(rule)
	outNode := state.AddNode("out.o", 0)
	edge2.inputs_ = []*Node{midNode}
	edge2.outputs_ = []*Node{outNode}

	// 设置计划
	plan.want[edge1] = WantToStart
	plan.want[edge2] = WantToStart

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
	inNode := state.AddNode("in.c", 0)
	outNode := state.AddNode("out.o", 0)
	edge.inputs_ = []*Node{inNode}
	edge.outputs_ = []*Node{outNode}

	// 输入就绪
	plan.want[edge] = WantToStart

	// 检查边是否就绪
	var err string
	plan.edgeMaybeReady(edge, WantToStart, &err)
	require.Equal(t, err, "")
}

// TestPlan_scheduleInitialEdges 测试初始边调度
func TestPlan_scheduleInitialEdges(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建立即可用的边
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	outNode := state.AddNode("out.o", 0)
	edge.outputs_ = []*Node{outNode}
	edge.inputs_ = []*Node{} // 无输入，立即可用

	plan.want[edge] = WantToStart

	// 调度初始边
	plan.scheduleInitialEdges()

	// 验证边被调度
	assert.Equal(t, 1, plan.ready.Len())
}

// TestPlan_AddTarget_Recursive 测试递归添加目标
func TestPlan_AddTarget_Recursive(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建多层依赖: a -> b -> c
	rule := &Rule{Name: "cc"}

	edge1 := state.AddEdge(rule)
	cNode := state.AddNode("c.c", 0)
	bNode := state.AddNode("b.o", 0)
	edge1.inputs_ = []*Node{cNode}
	edge1.outputs_ = []*Node{bNode}

	edge2 := state.AddEdge(rule)
	aNode := state.AddNode("a.o", 0)
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
	assert.Contains(t, plan.want, edge1)
	assert.Contains(t, plan.want, edge2)
}

// TestPlan_unmarkDependents 测试取消标记依赖
func TestPlan_unmarkDependents(t *testing.T) {
	state := NewState()
	plan := NewPlan(nil)

	// 创建图
	rule := &Rule{Name: "cc"}
	edge := state.AddEdge(rule)
	inNode := state.AddNode("in.c", 0)
	outNode := state.AddNode("out.o", 0)
	edge.inputs_ = []*Node{inNode}
	edge.outputs_ = []*Node{outNode}

	// 设置标记
	edge.mark_ = VisitDone
	plan.want[edge] = WantToStart

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
	outNode := state.AddNode("out.o", 0)
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
