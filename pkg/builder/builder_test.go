package builder

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"io"
	"ninja-go/pkg/util"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockCommandRunner 模拟命令执行
type mockCommandRunner struct {
	commandsRan []string
	failCommand map[string]bool // 哪些命令应失败
	fs          *mockFileSystem
}

func newMockCommandRunner(fs *mockFileSystem) *mockCommandRunner {
	return &mockCommandRunner{
		failCommand: make(map[string]bool),
		fs:          fs,
	}
}

// 不需要模拟文件效果，只记录命令即可：
func (r *mockCommandRunner) Run(cmdLine string, stdout, stderr io.Writer) error {
	r.commandsRan = append(r.commandsRan, cmdLine)
	if r.failCommand[cmdLine] {
		return &mockCommandError{msg: "mock command failed"}
	}
	// 模拟 cp 和 touch 命令
	parts := strings.Fields(cmdLine)
	if len(parts) >= 2 {
		switch parts[0] {
		case "cp":
			if len(parts) >= 3 {
				src := parts[1]
				dst := parts[2]
				if content, err := r.fs.ReadFile(src); err == nil {
					r.fs.WriteFile(dst, content, 0644)
				}
			}
		case "touch":
			for _, arg := range parts[1:] {
				r.fs.WriteFile(arg, []byte(""), 0644)
			}
		}
	}
	// 不需要模拟文件写入，由测试中的文件系统手动创建
	//// 模拟命令效果：解析简单的 cat 和 touch
	//if strings.Contains(cmdLine, "cat") && strings.Contains(cmdLine, ">") {
	//	// 简单解析：cat in > out
	//	parts := strings.Split(cmdLine, ">")
	//	if len(parts) == 2 {
	//		inPart := strings.TrimSpace(strings.TrimPrefix(parts[0], "cat"))
	//		inFile := strings.TrimSpace(inPart)
	//		outFile := strings.TrimSpace(parts[1])
	//		// 这里不实际写文件，由测试的 FileSystem mock 处理
	//		// 但我们需要将命令效果反馈到文件系统，需要访问 fs，暂不处理
	//	}
	//}
	return nil
}

type mockCommandError struct {
	msg string
}

func (e *mockCommandError) Error() string {
	return e.msg
}

type mockFile struct {
	*bytes.Buffer
	name   string
	closed bool
}

func (f *mockFile) Close() error { f.closed = true; return nil }
func (f *mockFile) Stat() (os.FileInfo, error) {
	return &mockFileInfoWrapper{path: f.name, mtime: 0}, nil
}

type mockFileSystem struct {
	files     map[string]*mockFileInfo
	nextMtime int64
}
type mockFileInfo struct {
	content string
	mtime   int64
}

func newMockFileSystem() *mockFileSystem {
	return &mockFileSystem{
		files:     make(map[string]*mockFileInfo),
		nextMtime: 1,
	}
}

func (fs *mockFileSystem) tick() {
	fs.nextMtime++
}

func (fs *mockFileSystem) Open(name string) (util.File, error) {
	if info, ok := fs.files[name]; ok {
		return &mockFile{Buffer: bytes.NewBufferString(info.content), name: name}, nil
	}
	return nil, os.ErrNotExist
}
func (fs *mockFileSystem) Create(name string) (util.File, error) {
	buf := &bytes.Buffer{}
	fs.files[name] = &mockFileInfo{content: "", mtime: fs.nextMtime}
	fs.nextMtime++
	return &mockFile{Buffer: buf, name: name}, nil
}

func (fs *mockFileSystem) Truncate(name string, size int64) error {
	if size < 0 {
		return os.ErrInvalid
	}
	info, ok := fs.files[name]
	if !ok {
		return os.ErrNotExist
	}
	current := []byte(info.content)
	if int64(len(current)) > size {
		// 截断
		info.content = string(current[:size])
	} else if int64(len(current)) < size {
		// 填充空字节
		padding := make([]byte, size-int64(len(current)))
		info.content = string(current) + string(padding)
	}
	// 更新 mtime
	info.mtime = fs.nextMtime
	fs.nextMtime++
	return nil
}

func (fs *mockFileSystem) Stat(path string) (os.FileInfo, error) {
	if f, ok := fs.files[path]; ok {
		return &mockFileInfoWrapper{path: path, mtime: f.mtime}, nil
	}
	return nil, os.ErrNotExist
}

func (fs *mockFileSystem) ReadFile(path string) ([]byte, error) {
	if f, ok := fs.files[path]; ok {
		return []byte(f.content), nil
	}
	return nil, os.ErrNotExist
}

func (fs *mockFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	fs.files[path] = &mockFileInfo{
		content: string(data),
		mtime:   fs.nextMtime,
	}
	fs.nextMtime++
	return nil
}

func (fs *mockFileSystem) Remove(path string) error {
	delete(fs.files, path)
	return nil
}

func (fs *mockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	// 简单模拟，不创建目录
	return nil
}

type mockFileInfoWrapper struct {
	path  string
	mtime int64
}

func (m *mockFileInfoWrapper) Name() string       { return m.path }
func (m *mockFileInfoWrapper) Size() int64        { return 0 }
func (m *mockFileInfoWrapper) Mode() os.FileMode  { return 0644 }
func (m *mockFileInfoWrapper) ModTime() time.Time { return time.Unix(m.mtime, 0) }
func (m *mockFileInfoWrapper) IsDir() bool        { return false }
func (m *mockFileInfoWrapper) Sys() interface{}   { return nil }

func TestNoWork(t *testing.T) {
	fs := newMockFileSystem()
	fs.WriteFile("in1", []byte("hello"), 0644)
	fs.WriteFile("out", []byte("hello"), 0644)

	state := NewState()
	rule := &Rule{Name: "cat", Command: "cat $in > $out"}
	edge := &Edge{Rule: rule}
	edge.Inputs = []*Node{state.AddNode("in1")}
	edge.Outputs = []*Node{state.AddNode("out")}
	state.Edges = append(state.Edges, edge)
	state.Defaults = []*Node{state.AddNode("out")}

	builder := NewBuilder(state, 1, newMockCommandRunner(fs), fs)
	err := builder.Build([]string{"default"})
	assert.NoError(t, err)
	// 验证没有命令运行
	mockRunner := builder.cmdRunner.(*mockCommandRunner)
	assert.Empty(t, mockRunner.commandsRan)
}

func TestOneStep(t *testing.T) {
	fs := newMockFileSystem()
	fs.WriteFile("in1", []byte("hello"), 0644)

	state := NewState()
	//rule := &graph.Rule{Name: "cat", Command: "cat $in > $out"}
	rule := &Rule{Name: "copy", Command: "cmd /c copy $in $out"} // Windows
	edge := &Edge{Rule: rule}
	inNode := state.AddNode("in1")
	outNode := state.AddNode("out")
	edge.Inputs = []*Node{inNode}
	edge.Outputs = []*Node{outNode}
	// 关键：设置输出节点的生成边
	outNode.Edge = edge
	outNode.Generated = true
	state.Edges = append(state.Edges, edge)
	state.Defaults = []*Node{outNode}

	builder := NewBuilder(state, 1, newMockCommandRunner(fs), fs)
	err := builder.Build([]string{"default"})
	assert.NoError(t, err)
	mockRunner := builder.cmdRunner.(*mockCommandRunner)
	assert.Len(t, mockRunner.commandsRan, 1)
	assert.Contains(t, mockRunner.commandsRan[0], `cmd \c copy in1 out`) // "cat in1 > out"
}

func TestTwoStep(t *testing.T) {
	fs := newMockFileSystem()
	fs.WriteFile("in1", []byte("hello"), 0644)

	state := NewState()
	rule := &Rule{Name: "cat", Command: "cmd /c copy $in $out"} // "cat $in > $out"
	// edge1: cat1 <- in1
	edge1 := &Edge{Rule: rule}
	in1Node := state.AddNode("in1")
	cat1Node := state.AddNode("cat1")
	outNode := state.AddNode("out")
	edge1.Inputs = []*Node{in1Node}
	edge1.Outputs = []*Node{cat1Node}
	// edge2: out <- cat1
	edge2 := &Edge{Rule: rule}
	edge2.Inputs = []*Node{cat1Node}
	edge2.Outputs = []*Node{outNode}
	state.Edges = append(state.Edges, edge1, edge2)
	// 关键：设置输出节点的生成边
	cat1Node.Edge = edge1
	cat1Node.Generated = true
	outNode.Edge = edge2
	outNode.Generated = true
	state.Defaults = []*Node{outNode}

	builder := NewBuilder(state, 1, newMockCommandRunner(fs), fs)
	err := builder.Build([]string{"default"})
	assert.NoError(t, err)
	mockRunner := builder.cmdRunner.(*mockCommandRunner)
	assert.Len(t, mockRunner.commandsRan, 2)
	assert.Equal(t, `cmd \c copy in1 cat1`, mockRunner.commandsRan[0]) // "cat in1 > cat1"
	assert.Equal(t, `cmd \c copy cat1 out`, mockRunner.commandsRan[1]) // "cat cat1 > out"
}

func TestMissingInput(t *testing.T) {
	fs := newMockFileSystem()
	// 不创建 in1

	state := NewState()
	rule := &Rule{Name: "cat", Command: "cmd /c copy $in $out"} // "cat $in > $out"
	edge := &Edge{Rule: rule}
	inNode := state.AddNode("in1")
	outNode := state.AddNode("out")
	edge.Inputs = []*Node{inNode}
	edge.Outputs = []*Node{outNode}
	// 关键：设置输出节点的生成边
	outNode.Edge = edge
	outNode.Generated = true
	state.Edges = append(state.Edges, edge)
	state.Defaults = []*Node{outNode}

	builder := NewBuilder(state, 1, newMockCommandRunner(fs), fs)
	err := builder.Build([]string{"default"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing input file")
}

func TestDepFileOK(t *testing.T) {
	fs := newMockFileSystem()
	fs.WriteFile("foo.c", []byte(""), 0644)
	fs.WriteFile("foo.o.d", []byte("foo.o: bar.h\n"), 0644)

	state := NewState()
	rule := &Rule{
		Name:    "cc",
		Command: "cc $in",
		Depfile: "$out.d",
	}
	edge := &Edge{Rule: rule}
	inNode := state.AddNode("foo.c")
	outNode := state.AddNode("foo.o")
	edge.Inputs = []*Node{inNode}
	edge.Outputs = []*Node{outNode}
	// 关键：设置输出节点的生成边
	outNode.Edge = edge
	outNode.Generated = true
	state.Edges = append(state.Edges, edge)
	state.Defaults = []*Node{outNode}

	builder := NewBuilder(state, 1, newMockCommandRunner(fs), fs)
	// 需要 builder.parseDepfile 被调用，这里通过构建触发
	err := builder.Build([]string{"default"})
	assert.NoError(t, err)
	// 验证隐式依赖 bar.h 被添加
	assert.NotNil(t, state.LookupNode("bar.h"))
	// 验证 edge 的隐式依赖包含 bar.h
	found := false
	for _, imp := range edge.ImplicitDeps {
		if imp.Path == "bar.h" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestPhony(t *testing.T) {
	fs := newMockFileSystem()
	fs.WriteFile("in1", []byte("hello"), 0644)

	state := NewState()
	ruleCat := &Rule{Name: "cat", Command: "cmd /c copy $in $out"} // "cat $in > $out"
	edge1 := &Edge{Rule: ruleCat}
	in1Node := state.AddNode("in1")
	outNode := state.AddNode("out")
	allNode := state.AddNode("all")
	edge1.Inputs = []*Node{in1Node}
	edge1.Outputs = []*Node{outNode}
	// phony 规则
	phonyRule := &Rule{Name: "phony"}
	edge2 := &Edge{Rule: phonyRule}
	edge2.Inputs = []*Node{outNode}
	edge2.Outputs = []*Node{allNode}
	//
	outNode.Edge = edge1
	outNode.Generated = true
	allNode.Edge = edge2
	allNode.Generated = true
	state.Edges = append(state.Edges, edge1, edge2)
	state.Defaults = []*Node{allNode}

	builder := NewBuilder(state, 1, newMockCommandRunner(fs), fs)
	err := builder.Build([]string{"default"})
	assert.NoError(t, err)
	mockRunner := builder.cmdRunner.(*mockCommandRunner)
	// 应该只运行了 cat，没有运行 phony 命令
	assert.Len(t, mockRunner.commandsRan, 1)
	assert.Contains(t, mockRunner.commandsRan[0], `copy in1 out`) // "cat in1 > out"
}

func TestFail(t *testing.T) {
	fs := newMockFileSystem()
	fs.WriteFile("in1", []byte("hello"), 0644)

	state := NewState()
	rule := &Rule{Name: "fail", Command: "fail"}
	edge := &Edge{Rule: rule}
	inNode := state.AddNode("in1")
	outNode := state.AddNode("out")
	edge.Inputs = []*Node{inNode}
	edge.Outputs = []*Node{outNode}
	//
	outNode.Edge = edge
	outNode.Generated = true
	state.Edges = append(state.Edges, edge)
	state.Defaults = []*Node{outNode}

	mockRunner := newMockCommandRunner(fs)
	mockRunner.failCommand["fail"] = true

	builder := NewBuilder(state, 1, mockRunner, fs)
	err := builder.Build([]string{"default"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command failed")
}

func TestPoolWithDepthOne(t *testing.T) {
	// 需要先实现 pool 功能，此测试占位
	t.Skip("pool concurrency not fully implemented")
}

// 测试用例：控制台池
func TestConsolePool(t *testing.T) {
	// 验证 console 池的行为
	t.Skip("console pool not implemented")
}

// 测试用例：响应文件（rspfile）
func TestRspFileSuccess(t *testing.T) {
	// 验证 rspfile 被创建并删除
	t.Skip("rspfile support not implemented")
}

// 测试用例：中断清理
func TestInterruptCleanup(t *testing.T) {
	// 验证中断时未完成的输出被删除
	t.Skip("interrupt handling not implemented")
}

// 测试用例：dyndep（动态依赖）
func TestDyndepBuild(t *testing.T) {
	fs := newMockFileSystem()
	ddContent := `ninja_dyndep_version = 1
build out | out.imp: dyndep | in.imp
`
	fs.WriteFile("dd-in", []byte(ddContent), 0644)
	fs.WriteFile("in", []byte(""), 0644)
	fs.WriteFile("in.imp", []byte(""), 0644) // 手动创建隐式依赖文件

	state := NewState()
	ruleCp := &Rule{Name: "cp", Command: "cp $in $out"}
	ruleTouch := &Rule{Name: "touch", Command: "touch $out"}

	// 边：生成 dd 文件
	edgeDd := &Edge{Rule: ruleCp}
	ddNode := state.AddNode("dd")
	ddInNode := state.AddNode("dd-in")
	edgeDd.Inputs = []*Node{ddInNode}
	edgeDd.Outputs = []*Node{ddNode}
	ddNode.Edge = edgeDd
	ddNode.Generated = true
	state.Edges = append(state.Edges, edgeDd)

	// 边：out 最初没有规则，将由 dyndep 动态添加隐式输入/输出
	// 注意：实际使用时，out 的边应已在解析 build.ninja 时存在，这里为了测试，先创建空边
	edgeOut := &Edge{Rule: ruleTouch}
	outNode := state.AddNode("out")
	edgeOut.Outputs = []*Node{outNode}
	edgeOut.DyndepFile = ddNode
	outNode.Edge = edgeOut
	outNode.Generated = true
	state.Edges = append(state.Edges, edgeOut)

	state.Defaults = []*Node{outNode}

	mockRunner := newMockCommandRunner(fs)
	builder := NewBuilder(state, 1, mockRunner, fs)
	err := builder.Build([]string{"default"})
	assert.NoError(t, err)
	// 验证隐式输入和输出已添加
	require.Len(t, edgeOut.ImplicitDeps, 1)
	assert.Equal(t, "in.imp", edgeOut.ImplicitDeps[0].Path)
	require.Len(t, edgeOut.Outputs, 2) // out 和 out.imp
	// 验证命令运行顺序
	require.Len(t, mockRunner.commandsRan, 2)
	assert.Equal(t, "cp dd-in dd", mockRunner.commandsRan[0])
	assert.Equal(t, "touch out out.imp", mockRunner.commandsRan[1])
}

// 测试用例：validation（验证边）
func TestValidation(t *testing.T) {
	// 验证 |@ 语法，确保验证边不影响主构建顺序
	t.Skip("validation syntax not implemented")
}
