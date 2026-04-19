package builder

import (
	"container/heap"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEdgePriorityQueue_Len 测试队列长度
func TestEdgePriorityQueue_Len(t *testing.T) {
	queue := &EdgePriorityQueue{}
	assert.Equal(t, 0, queue.Len())

	queue.edges = append(queue.edges, &Edge{Rule: &Rule{Name: "cc"}})
	assert.Equal(t, 1, queue.Len())

	queue.edges = append(queue.edges, &Edge{Rule: &Rule{Name: "link"}})
	assert.Equal(t, 2, queue.Len())
}

// TestEdgePriorityQueue_Less_ByWeight 测试按权重比较
func TestEdgePriorityQueue_Less_ByWeight(t *testing.T) {
	queue := &EdgePriorityQueue{}

	// 添加两条边，不同权重
	edge1 := &Edge{Rule: &Rule{Name: "cc"}, ID: 1, CriticalPathWeight: 10}
	edge2 := &Edge{Rule: &Rule{Name: "link"}, ID: 2, CriticalPathWeight: 20}
	queue.edges = []*Edge{edge1, edge2}

	// edge2 权重更高，应该排在前面（Less 返回 true 表示 i < j，即 i 优先）
	assert.True(t, queue.Less(1, 0)) // edge2 > edge1, so 1 < 0 is true
	assert.False(t, queue.Less(0, 1))
}

// TestEdgePriorityQueue_Less_ByID 测试按 ID 比较
func TestEdgePriorityQueue_Less_ByID(t *testing.T) {
	queue := &EdgePriorityQueue{}

	// 两条边权重相同，按 ID 升序
	edge1 := &Edge{Rule: &Rule{Name: "cc"}, ID: 2, CriticalPathWeight: 10}
	edge2 := &Edge{Rule: &Rule{Name: "cc"}, ID: 1, CriticalPathWeight: 10}
	queue.edges = []*Edge{edge1, edge2}

	// ID 小的优先
	assert.True(t, queue.Less(1, 0)) // edge2 (ID=1) < edge1 (ID=2)
	assert.False(t, queue.Less(0, 1))
}

// TestEdgePriorityQueue_Swap 测试交换
func TestEdgePriorityQueue_Swap(t *testing.T) {
	queue := &EdgePriorityQueue{}
	edge1 := &Edge{Rule: &Rule{Name: "cc"}, ID: 1}
	edge2 := &Edge{Rule: &Rule{Name: "link"}, ID: 2}
	queue.edges = []*Edge{edge1, edge2}

	queue.Swap(0, 1)

	assert.Equal(t, edge2, queue.edges[0])
	assert.Equal(t, edge1, queue.edges[1])
}

// TestEdgePriorityQueue_Push 测试推入
func TestEdgePriorityQueue_Push(t *testing.T) {
	queue := &EdgePriorityQueue{}
	edge := &Edge{Rule: &Rule{Name: "cc"}, ID: 1}

	queue.Push(edge)

	assert.Len(t, queue.edges, 1)
	assert.Equal(t, edge, queue.edges[0])
}

// TestEdgePriorityQueue_Pop 测试弹出
func TestEdgePriorityQueue_Pop(t *testing.T) {
	queue := &EdgePriorityQueue{}
	edge1 := &Edge{Rule: &Rule{Name: "cc"}, ID: 1}
	edge2 := &Edge{Rule: &Rule{Name: "link"}, ID: 2}

	queue.edges = []*Edge{edge1, edge2}

	// Pop 应该返回最后一条边（Pop 后 heap 会重新调整）
	result := queue.Pop().(*Edge)
	assert.Equal(t, edge2, result)
	assert.Len(t, queue.edges, 1)
}

// TestEdgePriorityQueue_Top 测试查看顶部
func TestEdgePriorityQueue_Top(t *testing.T) {
	queue := &EdgePriorityQueue{}

	// 空队列
	assert.Nil(t, queue.Top())

	// 添加边
	edge := &Edge{Rule: &Rule{Name: "cc"}, ID: 1}
	queue.edges = []*Edge{edge}

	assert.Equal(t, edge, queue.Top())
	// Top 不应该移除元素
	assert.Len(t, queue.edges, 1)
}

// TestEdgePriorityQueue_Clear 测试清空
func TestEdgePriorityQueue_Clear(t *testing.T) {
	queue := &EdgePriorityQueue{}
	queue.edges = []*Edge{
		{Rule: &Rule{Name: "cc"}},
		{Rule: &Rule{Name: "link"}},
	}

	queue.Clear()

	assert.Empty(t, queue.edges)
	assert.Equal(t, 0, queue.Len())
}

// TestEdgePriorityQueue_HeapInterface 测试堆接口
func TestEdgePriorityQueue_HeapInterface(t *testing.T) {
	queue := &EdgePriorityQueue{}
	heap.Init(queue)

	// 添加边，按优先级
	edges := []*Edge{
		{Rule: &Rule{Name: "a"}, ID: 1, CriticalPathWeight: 5},
		{Rule: &Rule{Name: "b"}, ID: 2, CriticalPathWeight: 10},
		{Rule: &Rule{Name: "c"}, ID: 3, CriticalPathWeight: 3},
		{Rule: &Rule{Name: "d"}, ID: 4, CriticalPathWeight: 10},
	}

	for _, e := range edges {
		heap.Push(queue, e)
	}

	assert.Equal(t, 4, queue.Len())

	// 弹出应该按权重降序，同权重按 ID 升序
	first := heap.Pop(queue).(*Edge)
	assert.Equal(t, int64(10), first.CriticalPathWeight)
	assert.Equal(t, uint64(2), first.ID)

	second := heap.Pop(queue).(*Edge)
	assert.Equal(t, int64(10), second.CriticalPathWeight)
	assert.Equal(t, uint64(4), second.ID)

	third := heap.Pop(queue).(*Edge)
	assert.Equal(t, int64(5), third.CriticalPathWeight)

	fourth := heap.Pop(queue).(*Edge)
	assert.Equal(t, int64(3), fourth.CriticalPathWeight)
}

// TestEdgePriorityQueue_PriorityOrder 测试优先级顺序
func TestEdgePriorityQueue_PriorityOrder(t *testing.T) {
	queue := &EdgePriorityQueue{}

	// 添加边，打乱顺序
	queue.Push(&Edge{Rule: &Rule{Name: "low"}, ID: 1, CriticalPathWeight: 1})
	queue.Push(&Edge{Rule: &Rule{Name: "high"}, ID: 2, CriticalPathWeight: 100})
	queue.Push(&Edge{Rule: &Rule{Name: "medium"}, ID: 3, CriticalPathWeight: 50})
	queue.Push(&Edge{Rule: &Rule{Name: "same_high"}, ID: 1, CriticalPathWeight: 100})

	// 堆化
	heap.Init(queue)

	// 弹出验证顺序
	top := queue.Top()
	assert.Equal(t, int64(100), top.CriticalPathWeight)
	assert.Equal(t, uint64(1), top.ID) // 同权重，ID 小的优先
}

// TestEdgePriorityQueue_EmptyPop 测试空队列弹出
func TestEdgePriorityQueue_EmptyPop(t *testing.T) {
	queue := &EdgePriorityQueue{}

	// 这会 panic，但这是预期的行为（与标准库 heap 一致）
	// 实际使用中应该先检查 Len()
	assert.Panics(t, func() {
		queue.Pop()
	})
}

// TestEdgePriorityQueue_MultipleOperations 测试多次操作
func TestEdgePriorityQueue_MultipleOperations(t *testing.T) {
	queue := &EdgePriorityQueue{}

	// 添加多条边
	for i := 0; i < 100; i++ {
		edge := &Edge{
			Rule:               &Rule{Name: "cc"},
			ID:                 uint64(i),
			CriticalPathWeight: int64(i % 10), // 0-9 的权重
		}
		heap.Push(queue, edge)
	}

	assert.Equal(t, 100, queue.Len())

	// 弹出所有边，验证顺序
	prevWeight := int64(10)
	count := 0
	for queue.Len() > 0 {
		edge := heap.Pop(queue).(*Edge)
		// 权重应该递减或相等
		assert.LessOrEqual(t, edge.CriticalPathWeight, prevWeight)
		prevWeight = edge.CriticalPathWeight
		count++
	}

	assert.Equal(t, 100, count)
}
