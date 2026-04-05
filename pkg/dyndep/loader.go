package dyndep

import (
	"fmt"
	"ninja-go/pkg/graph"
)

type FileReader interface {
	ReadFile(path string) ([]byte, error)
}

// DyndepInfo 存储从 dyndep 文件解析出的信息
type DyndepInfo struct {
	Restat          bool
	ImplicitInputs  []*graph.Node
	ImplicitOutputs []*graph.Node
}

// DyndepLoader 负责加载 dyndep 文件并更新图
type DyndepLoader struct {
	state     *graph.State
	dyndepMap map[*graph.Edge]*DyndepInfo
}

// NewDyndepLoader 创建加载器
func NewDyndepLoader(state *graph.State) *DyndepLoader {
	return &DyndepLoader{
		state:     state,
		dyndepMap: make(map[*graph.Edge]*DyndepInfo),
	}
}

func (l *DyndepLoader) LoadFromFile(fs FileReader, path string) error {
	data, err := fs.ReadFile(path)
	if err != nil {
		return err
	}
	return l.LoadFromContent(path, string(data))
}

func (l *DyndepLoader) LoadFromContent(filename, content string) error {
	p := NewDyndepParser(l.state, l)
	return p.Parse(filename, content)
}

// Apply 将解析出的 dyndep 信息应用到图中
func (l *DyndepLoader) Apply() error {
	for edge, info := range l.dyndepMap {
		if err := l.updateEdge(edge, info); err != nil {
			return err
		}
	}
	return nil
}

// updateEdge 更新边的隐式输入/输出，并处理 restat
func (dl *DyndepLoader) updateEdge(edge *graph.Edge, info *DyndepInfo) error {
	// 添加隐式输出
	for _, out := range info.ImplicitOutputs {
		// 检查是否已有其他边生成该输出
		if out.Edge != nil && out.Edge != edge {
			return fmt.Errorf("multiple rules generate %s", out.Path)
		}
		out.Edge = edge
		out.Generated = true
		edge.Outputs = append(edge.Outputs, out)
	}
	// 添加隐式输入（插入到显式输入和 order-only 之间）
	// 将隐式输入追加到 Inputs 末尾（保持顺序：显式、隐式、order-only）
	edge.ImplicitDeps = append(edge.ImplicitDeps, info.ImplicitInputs...)
	for _, in := range info.ImplicitInputs {
		in.AddOutEdge(edge)
	}
	// 处理 restat
	if info.Restat {
		edge.Rule.Restat = true
	}
	return nil
}
