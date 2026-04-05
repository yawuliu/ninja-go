package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"ninja-go/pkg/graph"
)

func TestExecutorRunNoEdges(t *testing.T) {
	exec := NewExecutor(2)
	err := exec.Run([]*graph.Edge{}, nil)
	assert.NoError(t, err)
}

// 更多测试可以在后续实现真正的并行执行时补充
