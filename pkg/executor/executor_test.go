package executor

import (
	"ninja-go/pkg/builder"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecutorRunNoEdges(t *testing.T) {
	exec := NewExecutor(2)
	err := exec.Run([]*builder.Edge{}, nil)
	assert.NoError(t, err)
}

// 更多测试可以在后续实现真正的并行执行时补充
