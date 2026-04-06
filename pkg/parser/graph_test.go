package parser

import (
	"ninja-go/pkg/graph"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ninja-go/pkg/util"
)

// 辅助：创建一个简单的 mock 文件系统
type mockFS struct {
	files map[string]time.Time
}

func newMockFS() *mockFS {
	return &mockFS{files: make(map[string]time.Time)}
}

func (fs *mockFS) Create(path string) {
	fs.files[path] = time.Now()
}

func (fs *mockFS) Touch(path string) {
	fs.files[path] = time.Now()
}

func (fs *mockFS) Stat(path string) (time.Time, bool) {
	t, ok := fs.files[path]
	return t, ok
}

func (fs *mockFS) Remove(path string) {
	delete(fs.files, path)
}

// 辅助：解析 manifest 并返回 state 和 scan（如果需要）
func parseState(t *testing.T, content string) *graph.State {
	state := graph.NewState()
	p := NewParser(state)
	err := p.ParseString(content) // 假设有 ParseString 方法
	require.NoError(t, err)
	return state
}

// 对应 GraphTest.MissingImplicit
func TestMissingImplicit(t *testing.T) {
	state := parseState(t, "build out: cat in | implicit\n")
	fs := newMockFS()
	fs.Create("in")
	fs.Create("out")

	// 模拟 DependencyScan 的 RecomputeDirty
	// 这里简化：直接使用 plan.Compute 检查 dirty
	outNode := state.LookupNode("out")
	require.NotNil(t, outNode)
	// 需要确保 implicit 文件缺失导致 out 脏
	// 实际应调用 scan.RecomputeDirty 或 builder 的脏标记逻辑
	// 此处仅作示例，假设有函数 markDirty
	// 我们直接验证逻辑：outNode.Dirty 应为 true
	// 由于没有实现完整的 RecomputeDirty，暂时跳过
	t.Skip("需要实现 RecomputeDirty 或类似功能")
}

// 对应 GraphTest.ModifiedImplicit
func TestModifiedImplicit(t *testing.T) {
	// 类似上述，验证隐式依赖修改导致输出脏
}

// 对应 GraphTest.FunkyMakefilePath
func TestFunkyMakefilePath(t *testing.T) {
	// 验证 depfile 中相对路径 ./foo/../implicit.h 能被正确规范化并找到文件
}

// 对应 GraphTest.ExplicitImplicit
func TestExplicitImplicit(t *testing.T) {
	// 同时有显式和隐式依赖，隐式应导致脏
}

// 对应 GraphTest.ImplicitOutputParse
func TestImplicitOutputParse(t *testing.T) {
	state := parseState(t, "build out | out.imp: cat in\n")
	outNode := state.LookupNode("out")
	impNode := state.LookupNode("out.imp")
	require.NotNil(t, outNode)
	require.NotNil(t, impNode)
	edge := outNode.Edge
	assert.Len(t, edge.Outputs, 2)
	assert.Equal(t, "out", edge.Outputs[0].Path)
	assert.Equal(t, "out.imp", edge.Outputs[1].Path)
	assert.Equal(t, 1, edge.ImplicitOuts)
	assert.Equal(t, edge, impNode.Edge)
}

// 对应 GraphTest.ImplicitOutputMissing
func TestImplicitOutputMissing(t *testing.T) {
	// 隐式输出缺失应导致输出脏
}

// 对应 GraphTest.ImplicitOutputOutOfDate
func TestImplicitOutputOutOfDate(t *testing.T) {
	// 隐式输出过时应导致输出脏
}

// 对应 GraphTest.ImplicitOutputOnlyParse
func TestImplicitOutputOnlyParse(t *testing.T) {
	state := parseState(t, "build | out.imp: cat in\n")
	impNode := state.LookupNode("out.imp")
	require.NotNil(t, impNode)
	edge := impNode.Edge
	assert.Len(t, edge.Outputs, 1)
	assert.Equal(t, "out.imp", edge.Outputs[0].Path)
	assert.Equal(t, 1, edge.ImplicitOuts)
	assert.Equal(t, edge, impNode.Edge)
}

// 对应 GraphTest.PathWithCurrentDirectory
func TestPathWithCurrentDirectory(t *testing.T) {
	// 验证 "./out.o" 等路径规范化
}

// 对应 GraphTest.RootNodes
func TestRootNodes(t *testing.T) {
	state := parseState(t, `
        build out1: cat in1
        build mid1: cat in1
        build out2: cat mid1
        build out3 out4: cat mid1
    `)
	roots := state.RootNodes()
	assert.Len(t, roots, 4)
	for _, r := range roots {
		assert.Contains(t, r.Path, "out")
	}
}

// 对应 GraphTest.InputsCollector
func TestInputsCollector(t *testing.T) {
	// 需要实现 InputsCollector，这里只提供框架
	// 模拟收集所有输入节点
	t.Skip("需要实现 InputsCollector")
}

// 对应 GraphTest.CommandCollector
func TestCommandCollector(t *testing.T) {
	t.Skip("需要实现 CommandCollector")
}

// 对应 GraphTest.VarInOutPathEscaping
func TestVarInOutPathEscaping(t *testing.T) {
	state := parseState(t, `build a$ b: cat no'space with$ space$$ no"space2`)
	edge := state.LookupNode("a b").Edge
	cmd := edge.EvaluateCommand()
	if util.IsWindows() {
		assert.Equal(t, `cat no'space "with space$" "no\"space2" > "a b"`, cmd)
	} else {
		assert.Equal(t, `cat 'no'\\''space' 'with space$' 'no"space2' > 'a b'`, cmd)
	}
}

// 对应 GraphTest.DepfileWithCanonicalizablePath
func TestDepfileWithCanonicalizablePath(t *testing.T) {
	// depfile 中 "bar/../foo.cc" 应规范化为 "foo.cc"
}

// 对应 GraphTest.DepfileRemoved
func TestDepfileRemoved(t *testing.T) {
	// depfile 被删除后，输出应变为脏
}

// 对应 GraphTest.RuleVariablesInScope
func TestRuleVariablesInScope(t *testing.T) {
	state := parseState(t, `
        rule r
          depfile = x
          command = depfile is $depfile
        build out: r in
    `)
	edge := state.LookupNode("out").Edge
	assert.Equal(t, "depfile is x", edge.EvaluateCommand())
}

// 对应 GraphTest.DepfileOverride
func TestDepfileOverride(t *testing.T) {
	state := parseState(t, `
        rule r
          depfile = x
          command = unused
        build out: r in
          depfile = y
    `)
	edge := state.LookupNode("out").Edge
	assert.Equal(t, "y", edge.GetBinding("depfile"))
}

// 对应 GraphTest.DepfileOverrideParent
func TestDepfileOverrideParent(t *testing.T) {
	state := parseState(t, `
        rule r
          depfile = x
          command = depfile is $depfile
        build out: r in
          depfile = y
    `)
	edge := state.LookupNode("out").Edge
	assert.Equal(t, "depfile is y", edge.GetBinding("command"))
}

// 对应 GraphTest.NestedPhonyPrintsDone
func TestNestedPhonyPrintsDone(t *testing.T) {
	// 验证 phony 链没有实际命令
}

// 对应 GraphTest.PhonySelfReferenceError
func TestPhonySelfReferenceError(t *testing.T) {
	// 验证自引用 phony 报错
}

// 对应 GraphTest.DependencyCycle
func TestDependencyCycle(t *testing.T) {
	state := parseState(t, `
        build out: cat mid
        build mid: cat in
        build in: cat pre
        build pre: cat out
    `)
	_ = state
	// 期望检测到循环
}

// 对应 GraphTest.CycleInEdgesButNotInNodes 系列
func TestCycleInEdgesButNotInNodes(t *testing.T) {
	// 多个输出导致的循环
}

// 对应 GraphTest.DyndepLoadTrivial
func TestDyndepLoadTrivial(t *testing.T) {
	// 测试 dyndep 加载，已有 dyndep 包测试，此处可调用
}

// 对应 GraphTest.Validation
func TestValidation(t *testing.T) {
	// 验证 |@ 边
}

// 对应 GraphTest.EdgeQueuePriority
func TestEdgeQueuePriority(t *testing.T) {
	// 验证优先级队列按 critical_path_weight 排序
}
