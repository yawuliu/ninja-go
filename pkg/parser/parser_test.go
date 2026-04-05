package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"ninja-go/pkg/graph"
)

func TestParseRule(t *testing.T) {
	input := `rule cc
  command = gcc -c $in -o $out
  depfile = $out.d`
	state := graph.NewState()
	p := NewParser(state)
	err := p.ParseReader(strings.NewReader(input), "hello.ninja")
	assert.NoError(t, err)
	assert.Contains(t, state.Rules, "cc")
	assert.Equal(t, "gcc -c $in -o $out", state.Rules["cc"].Command)
	assert.Equal(t, "$out.d", state.Rules["cc"].Depfile)
}
