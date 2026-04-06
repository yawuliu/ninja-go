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
func (l *DyndepLoader) LoadDyndeps(node *Node) error {
	var ddf DyndepFile
	return l.loadDyndeps(node, &ddf)
}

// loadDyndeps 内部实现，可传入已有的 DyndepFile 映射
func (l *DyndepLoader) loadDyndeps(node *Node, ddf *DyndepFile) error {
	// 标记不再等待 dyndep
	node.DyndepPending = false

	// 记录解释（可选）
	if l.explanations != nil {
		l.explanations.Record(node, "loading dyndep file '%s'", node.Path)
	}

	// 加载 dyndep 文件
	if err := l.loadDyndepFile(node, ddf); err != nil {
		return err
	}

	// 更新所有以该节点作为 dyndep 绑定的边
	for _, edge := range node.OutEdges {
		if edge.DyndepFile != node {
			continue
		}
		dyndeps, ok := (*ddf)[edge]
		if !ok {
			return fmt.Errorf("'%s' not mentioned in its dyndep file '%s'",
				edge.Outputs[0].Path, node.Path)
		}
		dyndeps.Used = true
		if err := l.UpdateEdge(edge, dyndeps); err != nil {
			return err
		}
	}

	// 拒绝 dyndep 文件中多余的边
	for edge, dyndeps := range *ddf {
		if !dyndeps.Used {
			return fmt.Errorf("dyndep file '%s' mentions output '%s' whose build statement does not have a dyndep binding for the file",
				node.Path, edge.Outputs[0].Path)
		}
	}
	return nil
}

// UpdateEdge 更新边，对应 C++ 的 UpdateEdge
func (l *DyndepLoader) UpdateEdge(edge *Edge, dyndeps *Dyndeps) error {
	// 添加 restat 绑定
	if dyndeps.Restat {
		// 假设 edge.Env 存在，且可以添加绑定
		if edge.Env == nil {
			edge.Env = NewBindingEnv(nil)
		}
		edge.Env.AddBinding("restat", "1")
	}

	// 添加隐式输出
	for _, out := range dyndeps.ImplicitOutputs {
		if out.InEdge != nil {
			return fmt.Errorf("multiple rules generate %s", out.Path)
		}
		out.InEdge = edge
		out.Generated = true
		edge.Outputs = append(edge.Outputs, out)
	}
	edge.ImplicitOuts += len(dyndeps.ImplicitOutputs)

	// 添加隐式输入（插入到 order-only 依赖之前）
	insertPos := len(edge.Inputs) - edge.OrderOnlyDeps
	newInputs := make([]*Node, 0, len(edge.Inputs)+len(dyndeps.ImplicitInputs))
	newInputs = append(newInputs, edge.Inputs[:insertPos]...)
	newInputs = append(newInputs, dyndeps.ImplicitInputs...)
	newInputs = append(newInputs, edge.Inputs[insertPos:]...)
	edge.Inputs = newInputs
	edge.ImplicitDeps += len(dyndeps.ImplicitInputs)

	// 添加反向边
	for _, in := range dyndeps.ImplicitInputs {
		in.AddOutEdge(edge)
	}
	return nil
}

// loadDyndepFile 读取文件内容并调用解析器。
func (l *DyndepLoader) loadDyndepFile(node *Node, ddf *DyndepFile) error {
	parser := NewDyndepParser(l.state, l.diskInterface, ddf)
	return parser.Load(node.Path)
}
