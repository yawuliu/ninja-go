package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewPool 测试 Pool 创建
func TestNewPool(t *testing.T) {
	pool := NewPool("test_pool", 4)
	assert.NotNil(t, pool)
	assert.Equal(t, "test_pool", pool.Name)
	assert.Equal(t, 4, pool.Depth)
	assert.Equal(t, 0, pool.currentUse)
	assert.Empty(t, pool.delayed)
}

// TestPool_ShouldDelayEdge 测试是否应该延迟边
func TestPool_ShouldDelayEdge(t *testing.T) {
	// 默认池（深度 0）不延迟
	defaultPool := NewPool("", 0)
	assert.False(t, defaultPool.ShouldDelayEdge())

	// 有限深度的池延迟
	limitedPool := NewPool("limited", 4)
	assert.True(t, limitedPool.ShouldDelayEdge())
}

// TestPool_EdgeScheduled 测试边调度
func TestPool_EdgeScheduled(t *testing.T) {
	pool := NewPool("test", 4)
	edge := &Edge{rule_: &Rule{Name: "cc"}}

	// 调度边
	pool.EdgeScheduled(edge)
	assert.Equal(t, 1, pool.currentUse)

	// 再调度一条边
	pool.EdgeScheduled(edge)
	assert.Equal(t, 2, pool.currentUse)
}

// TestPool_EdgeScheduled_Unlimited 测试无限池
func TestPool_EdgeScheduled_Unlimited(t *testing.T) {
	pool := NewPool("", 0)
	edge := &Edge{rule_: &Rule{Name: "cc"}}

	// 无限池不应该跟踪使用
	pool.EdgeScheduled(edge)
	assert.Equal(t, 0, pool.currentUse)
}

// TestPool_EdgeFinished 测试边完成
func TestPool_EdgeFinished(t *testing.T) {
	pool := NewPool("test", 4)
	edge := &Edge{rule_: &Rule{Name: "cc"}}

	// 先调度两条边
	pool.EdgeScheduled(edge)
	pool.EdgeScheduled(edge)
	assert.Equal(t, 2, pool.currentUse)

	// 完成一条边
	pool.EdgeFinished(edge)
	assert.Equal(t, 1, pool.currentUse)

	// 完成另一条边
	pool.EdgeFinished(edge)
	assert.Equal(t, 0, pool.currentUse)
}

// TestPool_DelayEdge 测试边延迟
func TestPool_DelayEdge(t *testing.T) {
	pool := NewPool("test", 2)
	edge1 := &Edge{rule_: &Rule{Name: "cc"}, id_: 1}
	edge2 := &Edge{rule_: &Rule{Name: "cc"}, id_: 2}

	// 延迟边
	pool.DelayEdge(edge1)
	pool.DelayEdge(edge2)

	assert.Len(t, pool.delayed, 2)
	assert.Equal(t, edge1, pool.delayed[0])
	assert.Equal(t, edge2, pool.delayed[1])
}

// TestPool_RetrieveReadyEdges 测试检索就绪边
func TestPool_RetrieveReadyEdges(t *testing.T) {
	pool := NewPool("test", 2)
	queue := &EdgePriorityQueue{}

	// 创建边
	edge1 := &Edge{rule_: &Rule{Name: "cc"}, id_: 1}
	edge2 := &Edge{rule_: &Rule{Name: "cc"}, id_: 2}
	edge3 := &Edge{rule_: &Rule{Name: "cc"}, id_: 3}

	// 延迟边
	pool.DelayEdge(edge1)
	pool.DelayEdge(edge2)
	pool.DelayEdge(edge3)

	// 当前使用为 0，应该能检索 2 条边（深度为 2）
	pool.RetrieveReadyEdges(queue)
	assert.Equal(t, 2, queue.Len())
	assert.Equal(t, 2, pool.currentUse)
	assert.Len(t, pool.delayed, 1)
	assert.Equal(t, edge3, pool.delayed[0])
}

// TestPool_RetrieveReadyEdges_WithCurrentUse 测试有当前使用时的检索
func TestPool_RetrieveReadyEdges_WithCurrentUse(t *testing.T) {
	pool := NewPool("test", 3)
	queue := &EdgePriorityQueue{}

	// 先使用一些容量
	edge1 := &Edge{rule_: &Rule{Name: "cc"}, id_: 1}
	pool.EdgeScheduled(edge1)
	assert.Equal(t, 1, pool.currentUse)

	// 延迟边
	edge2 := &Edge{rule_: &Rule{Name: "cc"}, id_: 2}
	edge3 := &Edge{rule_: &Rule{Name: "cc"}, id_: 3}
	pool.DelayEdge(edge2)
	pool.DelayEdge(edge3)

	// 还能检索 2 条边（深度 3 - 当前 1 = 2）
	pool.RetrieveReadyEdges(queue)
	assert.Equal(t, 2, queue.Len())
	assert.Equal(t, 3, pool.currentUse) // 1 + 2
	assert.Empty(t, pool.delayed)
}

// TestPool_RetrieveReadyEdges_Unlimited 测试无限池
func TestPool_RetrieveReadyEdges_Unlimited(t *testing.T) {
	pool := NewPool("", 0)
	queue := &EdgePriorityQueue{}

	edge := &Edge{rule_: &Rule{Name: "cc"}}
	pool.DelayEdge(edge)

	// 无限池不应该检索任何边
	pool.RetrieveReadyEdges(queue)
	assert.Equal(t, 0, queue.Len())
	assert.Len(t, pool.delayed, 1) // 边仍然在延迟列表中
}

// TestPool_ConcurrentAccess 测试并发访问
func TestPool_ConcurrentAccess(t *testing.T) {
	pool := NewPool("test", 10)
	edge := &Edge{rule_: &Rule{Name: "cc"}}

	// 并发调度
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			pool.EdgeScheduled(edge)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 10, pool.currentUse)

	// 并发完成
	for i := 0; i < 10; i++ {
		go func() {
			pool.EdgeFinished(edge)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 0, pool.currentUse)
}

// TestDefaultPool 测试默认池
func TestDefaultPool(t *testing.T) {
	assert.NotNil(t, DefaultPool)
	assert.Equal(t, "", DefaultPool.Name)
	assert.Equal(t, 0, DefaultPool.Depth)
}

// TestConsolePool 测试控制台池
func TestConsolePool_Creation(t *testing.T) {
	assert.NotNil(t, ConsolePool)
	assert.Equal(t, "console_", ConsolePool.Name)
	assert.Equal(t, 1, ConsolePool.Depth)
}
