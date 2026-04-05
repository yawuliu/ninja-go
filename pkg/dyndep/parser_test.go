package dyndep

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ninja-go/pkg/graph"
)

// 辅助函数：创建测试用的 State 和 Loader
func setupTestState() *graph.State {
	state := graph.NewState()
	// 添加规则 touch 和两个 build 语句（out 和 otherout）
	ruleTouch := &graph.Rule{Name: "touch", Command: "touch $out"}
	// 边 out
	edgeOut := &graph.Edge{Rule: ruleTouch}
	outNode := state.AddNode("out")
	edgeOut.Outputs = []*graph.Node{outNode}
	outNode.Edge = edgeOut
	outNode.Generated = true
	state.Edges = append(state.Edges, edgeOut)
	// 边 otherout
	edgeOther := &graph.Edge{Rule: ruleTouch}
	otherNode := state.AddNode("otherout")
	edgeOther.Outputs = []*graph.Node{otherNode}
	otherNode.Edge = edgeOther
	otherNode.Generated = true
	state.Edges = append(state.Edges, edgeOther)
	return state
}

// 辅助函数：解析内容并返回 loader 和错误
func parseContent(t *testing.T, state *graph.State, content string) (*DyndepLoader, error) {
	loader := NewDyndepLoader(state)
	parser := NewDyndepParser(state, loader)
	err := parser.Parse("input", content)
	return loader, err
}

// 辅助函数：断言错误信息包含子串
func assertErrorContains(t *testing.T, err error, substr string) {
	require.Error(t, err)
	assert.Contains(t, err.Error(), substr)
}

// 对应 DyndepParserTest.Empty
func TestEmpty(t *testing.T) {
	state := setupTestState()
	_, err := parseContent(t, state, "")
	assertErrorContains(t, err, "expected 'ninja_dyndep_version = ...'")
}

// 对应 DyndepParserTest.Version1
func TestVersion1(t *testing.T) {
	state := setupTestState()
	loader, err := parseContent(t, state, "ninja_dyndep_version = 1\n")
	require.NoError(t, err)
	assert.Empty(t, loader.dyndepMap)
}

// 对应 DyndepParserTest.Version1Extra
func TestVersion1Extra(t *testing.T) {
	state := setupTestState()
	loader, err := parseContent(t, state, "ninja_dyndep_version = 1-extra\n")
	require.NoError(t, err)
	assert.Empty(t, loader.dyndepMap)
}

// 对应 DyndepParserTest.Version1_0
func TestVersion1_0(t *testing.T) {
	state := setupTestState()
	loader, err := parseContent(t, state, "ninja_dyndep_version = 1.0\n")
	require.NoError(t, err)
	assert.Empty(t, loader.dyndepMap)
}

// 对应 DyndepParserTest.Version1_0Extra
func TestVersion1_0Extra(t *testing.T) {
	state := setupTestState()
	loader, err := parseContent(t, state, "ninja_dyndep_version = 1.0-extra\n")
	require.NoError(t, err)
	assert.Empty(t, loader.dyndepMap)
}

// 对应 DyndepParserTest.CommentVersion
func TestCommentVersion(t *testing.T) {
	content := "# comment\nninja_dyndep_version = 1\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	assert.Empty(t, loader.dyndepMap)
}

// 对应 DyndepParserTest.BlankLineVersion
func TestBlankLineVersion(t *testing.T) {
	content := "\nninja_dyndep_version = 1\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	assert.Empty(t, loader.dyndepMap)
}

// 对应 DyndepParserTest.VersionCRLF
func TestVersionCRLF(t *testing.T) {
	content := "ninja_dyndep_version = 1\r\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	assert.Empty(t, loader.dyndepMap)
}

// 对应 DyndepParserTest.CommentVersionCRLF
func TestCommentVersionCRLF(t *testing.T) {
	content := "# comment\r\nninja_dyndep_version = 1\r\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	assert.Empty(t, loader.dyndepMap)
}

// 对应 DyndepParserTest.BlankLineVersionCRLF
func TestBlankLineVersionCRLF(t *testing.T) {
	content := "\r\nninja_dyndep_version = 1\r\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	assert.Empty(t, loader.dyndepMap)
}

// 对应 DyndepParserTest.VersionUnexpectedEOF
func TestVersionUnexpectedEOF(t *testing.T) {
	content := "ninja_dyndep_version = 1.0"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "unexpected EOF")
}

// 对应 DyndepParserTest.UnsupportedVersion0
func TestUnsupportedVersion0(t *testing.T) {
	content := "ninja_dyndep_version = 0\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "unsupported")
}

// 对应 DyndepParserTest.UnsupportedVersion1_1
func TestUnsupportedVersion1_1(t *testing.T) {
	content := "ninja_dyndep_version = 1.1\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "unsupported")
}

// 对应 DyndepParserTest.DuplicateVersion
func TestDuplicateVersion(t *testing.T) {
	content := "ninja_dyndep_version = 1\nninja_dyndep_version = 1\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "duplicate")
}

// 对应 DyndepParserTest.MissingVersionOtherVar
func TestMissingVersionOtherVar(t *testing.T) {
	content := "not_ninja_dyndep_version = 1\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "expected 'ninja_dyndep_version = ...'")
}

// 对应 DyndepParserTest.MissingVersionBuild
func TestMissingVersionBuild(t *testing.T) {
	content := "build out: dyndep\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "expected 'ninja_dyndep_version = ...'")
}

// 对应 DyndepParserTest.UnexpectedEqual
func TestUnexpectedEqual(t *testing.T) {
	content := "= 1\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "unexpected '='")
}

// 对应 DyndepParserTest.UnexpectedIndent
func TestUnexpectedIndent(t *testing.T) {
	content := " = 1\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "unexpected indent")
}

// 对应 DyndepParserTest.OutDuplicate
func TestOutDuplicate(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep\nbuild out: dyndep\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "multiple statements for 'out'")
}

// 对应 DyndepParserTest.OutDuplicateThroughOther
func TestOutDuplicateThroughOther(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep\nbuild otherout: dyndep\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "multiple statements for 'otherout'")
}

// 对应 DyndepParserTest.NoOutEOF
func TestNoOutEOF(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "unexpected EOF")
}

// 对应 DyndepParserTest.NoOutColon
func TestNoOutColon(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild :\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "expected path")
}

// 对应 DyndepParserTest.OutNoStatement
func TestOutNoStatement(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild missing: dyndep\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "no build statement exists for 'missing'")
}

// 对应 DyndepParserTest.OutEOF
func TestOutEOF(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "unexpected EOF")
}

// 对应 DyndepParserTest.OutNoRule
func TestOutNoRule(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out:"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "expected 'dyndep' command")
}

// 对应 DyndepParserTest.OutBadRule
func TestOutBadRule(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: touch"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "expected 'dyndep' command")
}

// 对应 DyndepParserTest.BuildEOF
func TestBuildEOF(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "unexpected EOF")
}

// 对应 DyndepParserTest.ExplicitOut
func TestExplicitOut(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out exp: dyndep\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "explicit outputs not supported")
}

// 对应 DyndepParserTest.ExplicitIn
func TestExplicitIn(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep exp\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "explicit inputs not supported")
}

// 对应 DyndepParserTest.OrderOnlyIn
func TestOrderOnlyIn(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep ||\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "order-only inputs not supported")
}

// 对应 DyndepParserTest.BadBinding
func TestBadBinding(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep\n  not_restat = 1\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "binding is not 'restat'")
}

// 对应 DyndepParserTest.RestatTwice
func TestRestatTwice(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep\n  restat = 1\n  restat = 1\n"
	state := setupTestState()
	_, err := parseContent(t, state, content)
	assertErrorContains(t, err, "unexpected indent")
}

// 对应 DyndepParserTest.NoImplicit
func TestNoImplicit(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	require.Len(t, loader.dyndepMap, 1)
	for _, info := range loader.dyndepMap {
		assert.False(t, info.Restat)
		assert.Empty(t, info.ImplicitOutputs)
		assert.Empty(t, info.ImplicitInputs)
	}
}

// 对应 DyndepParserTest.EmptyImplicit
func TestEmptyImplicit(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out | : dyndep |\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	require.Len(t, loader.dyndepMap, 1)
	for _, info := range loader.dyndepMap {
		assert.False(t, info.Restat)
		assert.Empty(t, info.ImplicitOutputs)
		assert.Empty(t, info.ImplicitInputs)
	}
}

// 对应 DyndepParserTest.ImplicitIn
func TestImplicitIn(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep | impin\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	require.Len(t, loader.dyndepMap, 1)
	for _, info := range loader.dyndepMap {
		assert.Empty(t, info.ImplicitOutputs)
		require.Len(t, info.ImplicitInputs, 1)
		assert.Equal(t, "impin", info.ImplicitInputs[0].Path)
	}
}

// 对应 DyndepParserTest.ImplicitIns
func TestImplicitIns(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep | impin1 impin2\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	require.Len(t, loader.dyndepMap, 1)
	for _, info := range loader.dyndepMap {
		assert.Empty(t, info.ImplicitOutputs)
		require.Len(t, info.ImplicitInputs, 2)
		assert.Equal(t, "impin1", info.ImplicitInputs[0].Path)
		assert.Equal(t, "impin2", info.ImplicitInputs[1].Path)
	}
}

// 对应 DyndepParserTest.ImplicitOut
func TestImplicitOut(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out | impout: dyndep\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	require.Len(t, loader.dyndepMap, 1)
	for _, info := range loader.dyndepMap {
		require.Len(t, info.ImplicitOutputs, 1)
		assert.Equal(t, "impout", info.ImplicitOutputs[0].Path)
		assert.Empty(t, info.ImplicitInputs)
	}
}

// 对应 DyndepParserTest.ImplicitOuts
func TestImplicitOuts(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out | impout1 impout2 : dyndep\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	require.Len(t, loader.dyndepMap, 1)
	for _, info := range loader.dyndepMap {
		require.Len(t, info.ImplicitOutputs, 2)
		assert.Equal(t, "impout1", info.ImplicitOutputs[0].Path)
		assert.Equal(t, "impout2", info.ImplicitOutputs[1].Path)
		assert.Empty(t, info.ImplicitInputs)
	}
}

// 对应 DyndepParserTest.ImplicitInsAndOuts
func TestImplicitInsAndOuts(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out | impout1 impout2: dyndep | impin1 impin2\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	require.Len(t, loader.dyndepMap, 1)
	for _, info := range loader.dyndepMap {
		require.Len(t, info.ImplicitOutputs, 2)
		assert.Equal(t, "impout1", info.ImplicitOutputs[0].Path)
		assert.Equal(t, "impout2", info.ImplicitOutputs[1].Path)
		require.Len(t, info.ImplicitInputs, 2)
		assert.Equal(t, "impin1", info.ImplicitInputs[0].Path)
		assert.Equal(t, "impin2", info.ImplicitInputs[1].Path)
	}
}

// 对应 DyndepParserTest.Restat
func TestRestat(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild out: dyndep\n  restat = 1\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	require.Len(t, loader.dyndepMap, 1)
	for _, info := range loader.dyndepMap {
		assert.True(t, info.Restat)
		assert.Empty(t, info.ImplicitOutputs)
		assert.Empty(t, info.ImplicitInputs)
	}
}

// 对应 DyndepParserTest.OtherOutput
func TestOtherOutput(t *testing.T) {
	content := "ninja_dyndep_version = 1\nbuild otherout: dyndep\n"
	state := setupTestState()
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	require.Len(t, loader.dyndepMap, 1)
	for _, info := range loader.dyndepMap {
		assert.False(t, info.Restat)
		assert.Empty(t, info.ImplicitOutputs)
		assert.Empty(t, info.ImplicitInputs)
	}
}

// 对应 DyndepParserTest.MultipleEdges
func TestMultipleEdges(t *testing.T) {
	// 先添加第二个边 out2
	state := setupTestState()
	ruleTouch := &graph.Rule{Name: "touch", Command: "touch $out"}
	edgeOut2 := &graph.Edge{Rule: ruleTouch}
	out2Node := state.AddNode("out2")
	edgeOut2.Outputs = []*graph.Node{out2Node}
	out2Node.Edge = edgeOut2
	out2Node.Generated = true
	state.Edges = append(state.Edges, edgeOut2)

	content := "ninja_dyndep_version = 1\nbuild out: dyndep\nbuild out2: dyndep\n  restat = 1\n"
	loader, err := parseContent(t, state, content)
	require.NoError(t, err)
	require.Len(t, loader.dyndepMap, 2)

	// 查找 out 的边和 out2 的边
	var outEdge, out2Edge *graph.Edge
	for _, e := range state.Edges {
		if e.Outputs[0].Path == "out" {
			outEdge = e
		} else if e.Outputs[0].Path == "out2" {
			out2Edge = e
		}
	}
	require.NotNil(t, outEdge)
	require.NotNil(t, out2Edge)

	infoOut := loader.dyndepMap[outEdge]
	assert.False(t, infoOut.Restat)
	assert.Empty(t, infoOut.ImplicitOutputs)
	assert.Empty(t, infoOut.ImplicitInputs)

	infoOut2 := loader.dyndepMap[out2Edge]
	assert.True(t, infoOut2.Restat)
	assert.Empty(t, infoOut2.ImplicitOutputs)
	assert.Empty(t, infoOut2.ImplicitInputs)
}
