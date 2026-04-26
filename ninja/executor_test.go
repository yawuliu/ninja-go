package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecutorRunNoEdges(t *testing.T) {
	exec := NewExecutor(2)
	err := exec.Run([]*Edge{}, nil)
	assert.NoError(t, err)
}
