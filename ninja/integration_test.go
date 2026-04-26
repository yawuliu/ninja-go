package main

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockFSIntegration 用于集成测试的增强 mock 文件系统
type mockFSIntegration struct {
	files     map[string]string
	mtimes    map[string]int64
	nextMtime int64
}

func newMockFSIntegration() *mockFSIntegration {
	return &mockFSIntegration{
		files:     make(map[string]string),
		mtimes:    make(map[string]int64),
		nextMtime: 1000,
	}
}

func (m *mockFSIntegration) CreateFile(path string, content string) {
	m.files[path] = content
	m.mtimes[path] = m.nextMtime
	m.nextMtime++
}

func (m *mockFSIntegration) Touch(path string) {
	m.mtimes[path] = m.nextMtime
	m.nextMtime++
}

func (m *mockFSIntegration) ReadFile(path string) ([]byte, error) {
	if content, ok := m.files[path]; ok {
		return []byte(content), nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFSIntegration) WriteFile(path string, data []byte, perm os.FileMode) error {
	m.files[path] = string(data)
	m.mtimes[path] = m.nextMtime
	m.nextMtime++
	return nil
}

func (m *mockFSIntegration) Exists(path string) bool {
	_, ok := m.files[path]
	return ok
}

func (m *mockFSIntegration) Stat(path string) (os.FileInfo, error) {
	if _, ok := m.files[path]; ok {
		return &mockFileInfo{name: path, size: int64(len(m.files[path])), mtime: m.mtimes[path]}, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFSIntegration) Open(name string) (File, error) {
	return nil, os.ErrNotExist
}

func (m *mockFSIntegration) Create(name string) (File, error) {
	return nil, nil
}

func (m *mockFSIntegration) Truncate(name string, size int64) error {
	return nil
}

func (m *mockFSIntegration) Remove(path string) error {
	delete(m.files, path)
	return nil
}

func (m *mockFSIntegration) MkdirAll(path string, perm os.FileMode) error {
	return nil
}

func (m *mockFSIntegration) MakeDirs(path string) error {
	return nil
}

func (m *mockFSIntegration) AllowStatCache(allow bool) bool {
	return false
}

// mockFileInfo 用于测试的 mock 文件信息
type mockFileInfo struct {
	name  string
	size  int64
	mode  os.FileMode
	mtime int64
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return time.Unix(m.mtime, 0) }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() interface{}   { return nil }

// TestIntegration_SimpleBuild 测试简单构建
func TestIntegration_SimpleBuild(t *testing.T) {
	fs := newMockFSIntegration()
	fs.CreateFile("foo.c", "int main() { return 0; }")

	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out

build foo.o: cc foo.c
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 验证图结构
	require.Len(t, state.edges_, 1)
	edge := state.edges_[0]
	assert.Equal(t, "cc", edge.rule_.Name)
	assert.Len(t, edge.inputs_, 1)
	assert.Equal(t, "foo.c", edge.inputs_[0].path_)
	assert.Len(t, edge.outputs_, 1)
	assert.Equal(t, "foo.o", edge.outputs_[0].path_)
}

// TestIntegration_MultiStepBuild 测试多步构建
func TestIntegration_MultiStepBuild(t *testing.T) {
	fs := newMockFSIntegration()
	fs.CreateFile("main.c", "int main() { return 0; }")
	fs.CreateFile("util.c", "void util() {}")

	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc -c $in -o $out

rule link
  command = ld $in -o $out

build main.o: cc main.c
build util.o: cc util.c
build prog: link main.o util.o
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 验证边数量
	require.Len(t, state.edges_, 3)

	// 找到 link 边
	var linkEdge *Edge
	for _, e := range state.edges_ {
		if e.rule_.Name == "link" {
			linkEdge = e
			break
		}
	}
	require.NotNil(t, linkEdge)

	// link 应该有两个输入
	assert.Len(t, linkEdge.inputs_, 2)
}

// TestIntegration_ImplicitDeps 测试隐式依赖
func TestIntegration_ImplicitDeps(t *testing.T) {
	fs := newMockFSIntegration()
	fs.CreateFile("foo.c", "#include \"foo.h\"\n")
	fs.CreateFile("foo.h", "void foo();")

	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out
  depfile = $out.d

build foo.o: cc foo.c | foo.h
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	edge := state.edges_[0]
	assert.Len(t, edge.inputs_, 2)
	assert.Equal(t, 1, edge.implicit_deps_)
	assert.Equal(t, "foo.h", edge.inputs_[1].path_)
}

// TestIntegration_OrderOnlyDeps 测试 order-only 依赖
func TestIntegration_OrderOnlyDeps(t *testing.T) {
	fs := newMockFSIntegration()
	fs.CreateFile("foo.c", "")
	fs.CreateFile("stamp", "")

	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out

build foo.o: cc foo.c || stamp
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	edge := state.edges_[0]
	assert.Len(t, edge.inputs_, 2)
	assert.Equal(t, 1, edge.order_only_deps_)
	assert.True(t, edge.is_order_only(1))
}

// TestIntegration_VariableInheritance 测试变量继承
func TestIntegration_VariableInheritance(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
cc = gcc
cflags = -O2

rule cc
  command = $cc $cflags -c $in -o $out

build foo.o: cc foo.c
  cflags = -O3
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 验证全局变量
	assert.Equal(t, "gcc", state.bindings_.LookupVariable("cc"))
	assert.Equal(t, "-O2", state.bindings_.LookupVariable("cflags"))

	// 验证边特定变量
	edge := state.edges_[0]
	cmd := edge.EvaluateCommand(false)
	// 边特定的 cflags=-O3 应该覆盖全局的 -O2
	assert.Contains(t, cmd, "gcc")
	assert.Contains(t, cmd, "-O3")
}

// TestIntegration_DefaultTarget 测试默认目标
func TestIntegration_DefaultTarget(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out

build foo.o: cc foo.c
build bar.o: cc bar.c
build all: phony foo.o bar.o

default all
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 验证默认目标
	assert.Len(t, state.defaults_, 1)
	assert.Equal(t, "all", state.defaults_[0].path_)
}

// TestIntegration_PoolUsage 测试池使用
func TestIntegration_PoolUsage(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
pool link_pool
  depth = 2

rule link
  command = ld $in -o $out
  pool = link_pool

build prog: link foo.o
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	edge := state.edges_[0]
	assert.NotNil(t, edge.pool_)
	assert.Equal(t, "link_pool", edge.pool_.Name)
	assert.Equal(t, 2, edge.pool_.Depth)
}

// TestIntegration_Rspfile 测试响应文件
func TestIntegration_Rspfile(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule link
  command = ld @$rspfile -o $out
  rspfile = $out.rsp
  rspfile_content = $in

build prog: link foo.o bar.o baz.o
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	edge := state.edges_[0]
	rspfile := edge.GetUnescapedRspfile()
	assert.Equal(t, "prog.rsp", rspfile)

	rspContent := edge.GetBinding("rspfile_content")
	assert.NotEmpty(t, rspContent)
}

// TestIntegration_Dyndep 测试动态依赖
func TestIntegration_Dyndep(t *testing.T) {
	fs := newMockFSIntegration()
	fs.CreateFile("foo.c", "")
	fs.CreateFile("foo.dd", "ninja_dyndep_version = 1\nbuild foo.o: dyndep | foo.h\n")
	fs.CreateFile("foo.h", "")

	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out
  dyndep = $out.dd

build foo.o: cc foo.c foo.o.dd
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	edge := state.edges_[0]
	dyndep := edge.GetUnescapedDyndep()
	assert.Equal(t, "foo.o.dd", dyndep)

	// dyndep 文件节点应该被标记
	require.NotNil(t, edge.dyndep_)
	assert.True(t, edge.dyndep_.dyndep_pending_)
}

// TestIntegration_Phony 测试 phony 规则
func TestIntegration_Phony(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out

build foo.o: cc foo.c
build all: phony foo.o
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 找到 phony 边
	var phonyEdge *Edge
	for _, e := range state.edges_ {
		if e.rule_.Name == "phony" {
			phonyEdge = e
			break
		}
	}
	require.NotNil(t, phonyEdge)
	assert.True(t, phonyEdge.IsPhony())
}

// TestIntegration_ComplexGraph 测试复杂图结构
func TestIntegration_ComplexGraph(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
cc = gcc
cflags = -O2

rule cc
  command = $cc $cflags -MMD -MF $out.d -c $in -o $out
  depfile = $out.d

rule link
  command = $cc $in -o $out

rule ar
  command = ar rcs $out $in

# Object files
build main.o: cc main.c | config.h
build util.o: cc util.c | config.h
build helper.o: cc helper.c

# Library
build libutil.a: ar util.o helper.o

# Final binary
build myapp: link main.o libutil.a

default myapp
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 验证边数量
	assert.GreaterOrEqual(t, len(state.edges_), 4)

	// 验证默认目标
	assert.Len(t, state.defaults_, 1)
	assert.Equal(t, "myapp", state.defaults_[0].path_)
}

// TestIntegration_PathCanonicalization 测试路径规范化
func TestIntegration_PathCanonicalization(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out

build ./foo.o: cc ./foo.c
build bar/../baz.o: cc bar/../baz.c
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 路径应该被规范化
	assert.NotNil(t, state.LookupNode("foo.o"))
	assert.NotNil(t, state.LookupNode("baz.o"))
}

// TestIntegration_Validations 测试验证边
func TestIntegration_Validations(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out

rule check
  command = ./check.py $in

build foo.o: cc foo.c |@ check.py
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	edge := state.edges_[0]
	assert.Len(t, edge.validations_, 1)
	assert.Equal(t, "check.py", edge.validations_[0].path_)
}

// TestIntegration_RuleWithDescription 测试带描述的规则
func TestIntegration_RuleWithDescription(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out
  description = CC $out
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	rule := state.bindings_.LookupRule("cc")
	desc := rule.GetBinding("description")
	require.NotNil(t, desc)
	assert.Equal(t, "CC $out", desc.Unparse())
}

// TestIntegration_GeneratorRule 测试生成器规则
func TestIntegration_GeneratorRule(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule gen
  command = ./gen.py $in $out
  generator = 1

build config.h: gen config.in
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	rule := state.bindings_.LookupRule("gen")
	generator := rule.GetBinding("generator")
	require.NotNil(t, generator)
}

// TestIntegration_Restat 测试 restat 规则
func TestIntegration_Restat(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule stamp
  command = touch $out
  restat = 1

build stamp.txt: stamp always
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	rule := state.bindings_.LookupRule("stamp")
	restat := rule.GetBinding("restat")
	require.NotNil(t, restat)
}

// TestIntegration_MultipleOutputsPerEdge 测试单边多输出
func TestIntegration_MultipleOutputsPerEdge(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule bison
  command = bison $in $out

build parser.cc parser.h: bison parser.yy
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	edge := state.edges_[0]
	assert.Len(t, edge.outputs_, 2)
	assert.Equal(t, "parser.cc", edge.outputs_[0].path_)
	assert.Equal(t, "parser.h", edge.outputs_[1].path_)
}

// TestIntegration_EscapedVariables 测试转义变量
func TestIntegration_EscapedVariables(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule echo
  command = echo "Value: $$SOME_VAR"
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	rule := state.bindings_.LookupRule("echo")
	cmd := rule.GetBinding("command")
	require.NotNil(t, cmd)
	// $$ 应该被解析为 $
	assert.Contains(t, cmd.Unparse(), "$$SOME_VAR")
}

// TestIntegration_EmptyLines 测试空行处理
func TestIntegration_EmptyLines(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `

rule cc
  command = cc $in -o $out


build foo.o: cc foo.c


default foo.o

`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 应该正确解析
	assert.Len(t, state.edges_, 1)
}

// TestIntegration_Comments 测试注释
func TestIntegration_Comments(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
# This is a comment
rule cc
  command = cc $in -o $out
  # Another comment in rule
  description = CC $out

# Build statements
build foo.o: cc foo.c
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 注释应该被正确处理
	assert.Len(t, state.edges_, 1)
}

// TestIntegration_NinjaRequiredVersion 测试版本要求
func TestIntegration_NinjaRequiredVersion(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
ninja_required_version = 1.14

rule cc
  command = cc $in -o $out
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 版本应该被设置
	assert.Equal(t, 1, parser.lexer.manifestVersionMajor)
	assert.Equal(t, 14, parser.lexer.manifestVersionMinor)
}

// TestIntegration_BuildDir 测试构建目录
func TestIntegration_BuildDir(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
builddir = out

rule cc
  command = cc $in -o $out

build $builddir/foo.o: cc foo.c
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 输出路径应该包含 builddir
	assert.NotNil(t, state.LookupNode("out/foo.o"))
}

// TestIntegration_SubninjaIsolation 测试 subninja 作用域隔离
func TestIntegration_SubninjaIsolation(t *testing.T) {
	fs := newMockFSIntegration()
	fs.CreateFile("sub.ninja", `
local_var = in_sub
rule cc
  command = cc $in -o $out
build sub.o: cc sub.c
`)

	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
global_var = before
subninja sub.ninja
global_var = after
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// subninja 中的变量不应该影响父级
	assert.Equal(t, "", state.bindings_.LookupVariable("local_var"))
}

// TestIntegration_IncludeSharing 测试 include 作用域共享
func TestIntegration_IncludeSharing(t *testing.T) {
	fs := newMockFSIntegration()
	fs.CreateFile("common.ninja", `
common_var = shared
`)

	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
include common.ninja
rule cc
  command = $common_var $in -o $out
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// include 中的变量应该可用
	assert.Equal(t, "shared", state.bindings_.LookupVariable("common_var"))
}

// TestIntegration_ConcurrentBuild 测试并发构建准备
func TestIntegration_ConcurrentBuild(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out

build a.o: cc a.c
build b.o: cc b.c
build c.o: cc c.c
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 所有边应该是独立的（没有依赖关系）
	for _, edge := range state.edges_ {
		// 每个边只有一个输入（源文件）
		assert.Len(t, edge.inputs_, 1)
		// 每个边只有一个输出
		assert.Len(t, edge.outputs_, 1)
	}
}

// TestIntegration_ChainBuild 测试链式构建
func TestIntegration_ChainBuild(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule gen
  command = gen $in $out

rule process
  command = process $in $out

rule finalize
  command = finalize $in $out

build step1.out: gen input.txt
build step2.out: process step1.out
build final.out: finalize step2.out
`
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 验证依赖链
	require.Len(t, state.edges_, 3)

	// step2 依赖于 step1
	step2 := state.LookupNode("step2.out")
	require.NotNil(t, step2)
	require.NotNil(t, step2.in_edge())
	assert.Equal(t, "process", step2.in_edge().rule_.Name)

	// final 依赖于 step2
	final := state.LookupNode("final.out")
	require.NotNil(t, final)
	require.NotNil(t, final.in_edge())
	assert.Equal(t, "finalize", final.in_edge().rule_.Name)
}

// TestIntegration_CircularDependency 测试循环依赖检测
func TestIntegration_CircularDependency(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	manifest := `
rule cc
  command = cc $in -o $out

# 循环依赖: a -> b -> a
build a.o: cc b.o
build b.o: cc a.o
`
	// 解析应该成功，循环检测在构建阶段
	var err string
	parser.ParseTest(manifest, &err)
	require.Equal(t, err, "")

	// 验证两条边都被创建
	assert.Len(t, state.edges_, 2)
}
