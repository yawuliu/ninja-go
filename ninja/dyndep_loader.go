package main

import (
	"fmt"
	"ninja-go/ninja/util"
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
	state         *State
	diskInterface util.FileSystem // 文件读取接口
	explanations  *Explanations
}

func NewDyndepLoader(state *State, diskInterface util.FileSystem) *DyndepLoader {
	return &DyndepLoader{
		state:         state,
		diskInterface: diskInterface,
	}
}

// LoadDyndeps 加载 dyndep 文件，更新图
func (l *DyndepLoader) LoadDyndeps(node *Node, err *string) bool {
	var ddf DyndepFile
	return l.loadDyndeps(node, &ddf, err)
}

// loadDyndeps 内部实现，可传入已有的 DyndepFile 映射
func (l *DyndepLoader) loadDyndeps(node *Node, ddf *DyndepFile, err *string) bool {
	// 标记不再等待 dyndep
	node.DyndepPending = false

	// 记录解释（可选）
	if l.explanations != nil {
		l.explanations.Record(node, "loading dyndep log_file_ '%s'", node.Path)
	}

	// 加载 dyndep 文件
	if !l.loadDyndepFile(node, ddf, err) {
		return false
	}

	// 更新所有以该节点作为 dyndep 绑定的边
	for _, edge := range node.OutEdges {
		if edge.dyndep_ != node {
			continue
		}
		dyndeps, ok := (*ddf)[edge]
		if !ok {
			*err = fmt.Sprintf("'%s' not mentioned in its dyndep log_file_ '%s'",
				edge.outputs_[0].Path, node.Path)
			return false
		}
		dyndeps.Used = true
		if !l.UpdateEdge(edge, dyndeps, err) {
			return false
		}
	}

	// 拒绝 dyndep 文件中多余的边
	for edge, dyndeps := range *ddf {
		if !dyndeps.Used {
			*err = fmt.Sprintf("dyndep log_file_ '%s' mentions output '%s' whose build statement does not have a dyndep binding for the log_file_",
				node.Path, edge.outputs_[0].Path)
			return false
		}
	}
	return true
}

// UpdateEdge 更新边，对应 C++ 的 UpdateEdge
func (l *DyndepLoader) UpdateEdge(edge *Edge, dyndeps *Dyndeps, err *string) bool {
	// 添加 restat 绑定（如果 dyndep 中指定了 restat = 1）
	if dyndeps.Restat {
		// 确保边有自己的 BindingEnv（通常在解析时已创建）
		if edge.env_ == nil {
			edge.env_ = NewBindingEnv(nil)
		}
		edge.env_.AddBinding("restat", "1")
	}

	// 添加隐式输出到边的输出列表末尾
	edge.outputs_ = append(edge.outputs_, dyndeps.ImplicitOutputs...)
	edge.implicit_outs_ += len(dyndeps.ImplicitOutputs)

	// 为每个隐式输出设置产生它的边（如果已经被其他边产生，则报错）
	for _, out := range dyndeps.ImplicitOutputs {
		if out.InEdge != nil {
			*err = fmt.Sprintf("multiple rules generate %s", out.Path)
			return false
		}
		out.set_in_edge(edge)
	}

	// 添加隐式输入：插入到现有输入列表的末尾（order-only 依赖之前）
	// 计算插入位置：当前输入长度减去 order-only 依赖数量
	insertPos := len(edge.inputs_) - edge.order_only_deps_
	if insertPos < 0 {
		insertPos = 0
	}
	newInputs := make([]*Node, 0, len(edge.inputs_)+len(dyndeps.ImplicitInputs))
	newInputs = append(newInputs, edge.inputs_[:insertPos]...)
	newInputs = append(newInputs, dyndeps.ImplicitInputs...)
	newInputs = append(newInputs, edge.inputs_[insertPos:]...)
	edge.inputs_ = newInputs
	edge.implicit_deps_ += len(dyndeps.ImplicitInputs)

	// 为每个隐式输入添加反向边（该边依赖于这些输入）
	for _, in := range dyndeps.ImplicitInputs {
		in.AddOutEdge(edge)
	}

	return true
}

// loadDyndepFile 读取文件内容并调用解析器。
func (l *DyndepLoader) loadDyndepFile(node *Node, ddf *DyndepFile, err *string) bool {
	parser := NewDyndepParser(l.state, l.diskInterface, ddf)
	return parser.Load(node.Path, err, nil)
}
