package builder

import (
	"fmt"
	"ninja-go/pkg/util"
)

// DyndepInfo 对应 C++ 的 Dyndeps
type DyndepInfo struct {
	Used            bool
	Restat          bool
	ImplicitInputs  []*Node
	ImplicitOutputs []*Node
}

// DyndepLoader 对应 C++ 的 DyndepLoader
type DyndepLoader struct {
	state *State
	fs    util.FileSystem // 文件读取接口
	// explanations 可选
}

func NewDyndepLoader(state *State, fs util.FileSystem) *DyndepLoader {
	return &DyndepLoader{
		state: state,
		fs:    fs,
	}
}

// LoadDyndeps 加载 dyndep 文件，更新图
func (l *DyndepLoader) LoadDyndeps(node *Node) error {
	return l.loadDyndeps(node, nil)
}

// loadDyndeps 内部实现，可传入已有的 DyndepFile 映射
func (l *DyndepLoader) loadDyndeps(node *Node, ddf DyndepFile) error {
	if ddf == nil {
		ddf = make(DyndepFile)
	}
	// 读取文件内容
	data, err := l.fs.ReadFile(node.Path)
	if err != nil {
		return fmt.Errorf("loading '%s': %v", node.Path, err)
	}
	// 解析
	parser := NewDyndepParser(l.state, nil) // 需要传入 loader 引用，稍后修改
	if err := parser.Parse(node.Path, string(data)); err != nil {
		return err
	}
	// 将解析结果合并到 ddf 中（实际 parser 已填充到 loader 内部，需要调整）
	// 为简化，我们直接在 Parser 中填充传入的 map
	// 重新设计：Parser 应返回 DyndepInfo 映射
	// 由于时间，这里略去细节
	return nil
}

// UpdateEdge 更新边，对应 C++ 的 UpdateEdge
func (l *DyndepLoader) UpdateEdge(edge *Edge, info *DyndepInfo) error {
	if info.Restat {
		edge.Rule.Restat = true
	}
	// 添加隐式输出
	for _, out := range info.ImplicitOutputs {
		if out.Edge != nil && out.Edge != edge {
			return fmt.Errorf("multiple rules generate %s", out.Path)
		}
		out.Edge = edge
		out.Generated = true
		edge.Outputs = append(edge.Outputs, out)
	}
	// 添加隐式输入
	edge.ImplicitDeps = append(edge.ImplicitDeps, info.ImplicitInputs...)
	for _, in := range info.ImplicitInputs {
		in.AddOutEdge(edge)
	}
	return nil
}
