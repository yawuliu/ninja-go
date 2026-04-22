package main

import (
	"ninja-go/ninja/util"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockFileSystemForParser 用于解析器测试的 mock 文件系统
type mockFileSystemForParser struct {
	files map[string][]byte
}

func newMockFileSystemForParser() *mockFileSystemForParser {
	return &mockFileSystemForParser{
		files: make(map[string][]byte),
	}
}

func (m *mockFileSystemForParser) Open(name string) (util.File, error) {
	return nil, os.ErrNotExist
}

func (m *mockFileSystemForParser) Create(name string) (util.File, error) {
	return nil, nil
}

func (m *mockFileSystemForParser) Truncate(name string, size int64) error {
	return nil
}

func (m *mockFileSystemForParser) Stat(path string) (os.FileInfo, error) {
	return nil, os.ErrNotExist
}

func (m *mockFileSystemForParser) ReadFile(path string) ([]byte, error) {
	if data, ok := m.files[path]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFileSystemForParser) WriteFile(path string, data []byte, perm os.FileMode) error {
	m.files[path] = data
	return nil
}

func (m *mockFileSystemForParser) Remove(path string) error {
	delete(m.files, path)
	return nil
}

func (m *mockFileSystemForParser) MkdirAll(path string, perm os.FileMode) error {
	return nil
}

func (m *mockFileSystemForParser) MakeDirs(path string) error {
	return nil
}

func (m *mockFileSystemForParser) AllowStatCache(allow bool) bool {
	return false
}

func (m *mockFileSystemForParser) AddFile(path string, content string) {
	m.files[path] = []byte(content)
}

func TestManifestParser_ParseEmpty(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	var err string
	parser.ParseTest("", &err)
	require.Equal(t, err, "")
}

func TestManifestParser_ParseRule(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $in -o $out
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 验证规则被添加
	rule := state.Bindings.LookupRule("cc")
	require.NotNil(t, rule)
	assert.Equal(t, "cc", rule.Name)
}

func TestManifestParser_ParseRuleWithMultipleBindings(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $in -o $out
  description = CC $out
  depfile = $out.d
  deps = gcc
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	rule := state.Bindings.LookupRule("cc")
	require.NotNil(t, rule)

	cmd := rule.GetBinding("command")
	require.NotNil(t, cmd)
	assert.Equal(t, "gcc $in -o $out", cmd.Unparse())
}

func TestManifestParser_ParseRule_Duplicate(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $in -o $out

rule cc
  command = clang $in -o $out
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "duplicate rule")
}

func TestManifestParser_ParseRule_MissingCommand(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  description = CC $out
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "expected 'command'")
}

func TestManifestParser_ParseRule_RspfileMismatch(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc @$rspfile -o $out
  rspfile = $out.rsp
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "rspfile and rspfile_content need to be both specified")
}

func TestManifestParser_ParseBuild(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $in -o $out

build foo.o: cc foo.c
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 验证边被创建
	require.Len(t, state.Edges, 1)
	edge := state.Edges[0]
	assert.Equal(t, "cc", edge.Rule.Name)
	require.Len(t, edge.inputs_, 1)
	assert.Equal(t, "foo.c", edge.inputs_[0].Path)
	require.Len(t, edge.outputs_, 1)
	assert.Equal(t, "foo.o", edge.outputs_[0].Path)
}

func TestManifestParser_ParseBuild_MultipleInputs(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule link
  command = ld $in -o $out

build prog: link foo.o bar.o baz.o
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	edge := state.Edges[0]
	require.Len(t, edge.inputs_, 3)
	assert.Equal(t, "foo.o", edge.inputs_[0].Path)
	assert.Equal(t, "bar.o", edge.inputs_[1].Path)
	assert.Equal(t, "baz.o", edge.inputs_[2].Path)
}

func TestManifestParser_ParseBuild_MultipleOutputs(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule gen
  command = gen $in

build out1 out2 out3: gen input.txt
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	edge := state.Edges[0]
	require.Len(t, edge.outputs_, 3)
	assert.Equal(t, "out1", edge.outputs_[0].Path)
	assert.Equal(t, "out2", edge.outputs_[1].Path)
	assert.Equal(t, "out3", edge.outputs_[2].Path)
}

func TestManifestParser_ParseBuild_ImplicitInputs(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $in -o $out

build foo.o: cc foo.c | header.h config.h
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	edge := state.Edges[0]
	require.Len(t, edge.inputs_, 3)
	assert.Equal(t, "foo.c", edge.inputs_[0].Path)
	assert.Equal(t, "header.h", edge.inputs_[1].Path)
	assert.Equal(t, "config.h", edge.inputs_[2].Path)
	assert.Equal(t, 2, edge.implicit_deps_)
}

func TestManifestParser_ParseBuild_OrderOnlyInputs(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $in -o $out

build foo.o: cc foo.c || stamp
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	edge := state.Edges[0]
	require.Len(t, edge.inputs_, 2)
	assert.Equal(t, "foo.c", edge.inputs_[0].Path)
	assert.Equal(t, "stamp", edge.inputs_[1].Path)
	assert.Equal(t, 1, edge.order_only_deps_)
}

func TestManifestParser_ParseBuild_ImplicitOutputs(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule gen
  command = gen $in

build foo.o | foo.h: gen foo.c
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	edge := state.Edges[0]
	require.Len(t, edge.outputs_, 2)
	assert.Equal(t, "foo.o", edge.outputs_[0].Path)
	assert.Equal(t, "foo.h", edge.outputs_[1].Path)
	assert.Equal(t, 1, edge.implicit_outs_)
}

func TestManifestParser_ParseBuild_Validations(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $in -o $out

build foo.o: cc foo.c |@ validate.py
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	edge := state.Edges[0]
	require.Len(t, edge.validations_, 1)
	assert.Equal(t, "validate.py", edge.validations_[0].Path)
}

func TestManifestParser_ParseBuild_EdgeVariables(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $flags $in -o $out

build foo.o: cc foo.c
  flags = -O2 -Wall
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	edge := state.Edges[0]
	cmd := edge.EvaluateCommand(false)
	assert.Equal(t, "gcc -O2 -Wall foo.c -o foo.o", cmd)
}

func TestManifestParser_ParseBuild_UnknownRule(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `build foo.o: unknown foo.c
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "unknown build rule")
}

func TestManifestParser_ParseBuild_MissingOutputs(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $in -o $out

build : cc foo.c
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "expected path")
}

func TestManifestParser_ParseDefault(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $in -o $out

build foo.o: cc foo.c

default foo.o
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 验证默认目标
	require.Len(t, state.Defaults, 1)
	assert.Equal(t, "foo.o", state.Defaults[0].Path)
}

func TestManifestParser_ParseDefault_Multiple(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = gcc $in -o $out

build foo.o: cc foo.c
build bar.o: cc bar.c

default foo.o bar.o
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	require.Len(t, state.Defaults, 2)
	assert.Equal(t, "foo.o", state.Defaults[0].Path)
	assert.Equal(t, "bar.o", state.Defaults[1].Path)
}

func TestManifestParser_ParseDefault_UnknownTarget(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `default nonexistent
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "unknown target")
}

func TestManifestParser_ParsePool(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `pool link_pool
  depth = 4
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 验证池被创建
	pool := state.LookupPool("link_pool")
	require.NotNil(t, pool)
	assert.Equal(t, "link_pool", pool.Name)
	assert.Equal(t, 4, pool.Depth)
}

func TestManifestParser_ParsePool_Duplicate(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `pool mypool
  depth = 2

pool mypool
  depth = 4
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "duplicate pool")
}

func TestManifestParser_ParsePool_MissingDepth(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `pool mypool
  other = value
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "expected 'depth'")
}

func TestManifestParser_ParsePool_InvalidDepth(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `pool mypool
  depth = -1
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "invalid pool depth")
}

func TestManifestParser_ParseVariable(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `cc = gcc
flags = -O2
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 验证变量被设置
	assert.Equal(t, "gcc", state.Bindings.LookupVariable("cc"))
	assert.Equal(t, "-O2", state.Bindings.LookupVariable("flags"))
}

func TestManifestParser_ParseVariable_Reference(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `cc = gcc
ccflags = -O2
command = $cc $ccflags
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 验证变量引用被正确展开
	cmd := state.Bindings.LookupVariable("command")
	assert.Equal(t, "gcc -O2", cmd)
}

func TestManifestParser_ParseVersion(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `ninja_required_version = 1.14
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 验证版本被设置到 lexer
	assert.Equal(t, 1, parser.lexer.manifestVersionMajor)
	assert.Equal(t, 14, parser.lexer.manifestVersionMinor)
}

func TestManifestParser_ParseInclude(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	fs.AddFile("included.ninja", "cc = gcc\n")
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `include included.ninja
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 验证包含的文件被解析
	assert.Equal(t, "gcc", state.Bindings.LookupVariable("cc"))
}

func TestManifestParser_ParseSubninja(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	fs.AddFile("sub.ninja", "local = value\n")
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `global = before
subninja sub.ninja
global = after
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// subninja 中的变量不应该影响父级
	assert.Equal(t, "", state.Bindings.LookupVariable("local"))
	assert.Equal(t, "after", state.Bindings.LookupVariable("global"))
}

func TestManifestParser_ParseComments(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `# This is a comment
rule cc
  command = gcc $in -o $out
  # Another comment
  description = CC $out
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 验证规则被正确解析
	rule := state.Bindings.LookupRule("cc")
	require.NotNil(t, rule)
}

func TestManifestParser_ParseBuild_WithPool(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `pool link_pool
  depth = 2

rule link
  command = ld $in -o $out
  pool = link_pool

build prog: link foo.o bar.o
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	edge := state.Edges[0]
	require.NotNil(t, edge.Pool)
	assert.Equal(t, "link_pool", edge.Pool.Name)
}

func TestManifestParser_ParseBuild_UnknownPool(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule link
  command = ld $in -o $out
  pool = unknown_pool

build prog: link foo.o
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "unknown pool name")
}

func TestManifestParser_ParseBuild_Phony(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `build all: phony foo.o bar.o
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	edge := state.Edges[0]
	assert.Equal(t, "phony", edge.Rule.Name)
}

func TestManifestParser_ParseBuild_PhonyCycleWarn(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{PhonyCycleAction: PhonyCycleActionWarn})

	input := `build all: phony all
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 自引用应该被处理（警告模式下）
	edge := state.Edges[0]
	assert.Len(t, edge.inputs_, 0) // 自引用被移除
}

func TestManifestParser_ParseComplex(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `cc = gcc
cflags = -O2 -Wall

rule cc
  command = $cc $cflags $in -o $out
  description = CC $out
  depfile = $out.d

rule link
  command = $cc $in -o $out
  description = LINK $out

build foo.o: cc foo.c
build bar.o: cc bar.c

build prog: link foo.o bar.o

default prog
`
	var err string
	parser.ParseTest(input, &err)
	require.Equal(t, err, "")

	// 验证所有边被创建
	assert.Len(t, state.Edges, 3)

	// 验证默认目标
	assert.Len(t, state.Defaults, 1)
	assert.Equal(t, "prog", state.Defaults[0].Path)
}

func TestManifestParser_ParseError_SyntaxError(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `build foo.o cc foo.c
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
}

func TestManifestParser_ParseError_EmptyPath(t *testing.T) {
	state := NewState()
	fs := newMockFileSystemForParser()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})

	input := `rule cc
  command = $in -o $out

build $: cc foo.c
`
	var err string
	parser.ParseTest(input, &err)
	require.NotEqual(t, err, "")
	assert.Contains(t, err, "empty path")
}
