package builder

import (
	"fmt"
	"ninja-go/pkg/util"
	"strings"
)

type DependencyScan struct {
	state           *State
	buildLog        *BuildLog
	depsLog         *DepsLog
	disk_interface_ util.FileSystem
	depLoader       *ImplicitDepLoader
	dyndepLoader    *DyndepLoader
	explanations_   *OptionalExplanations
}

func NewDependencyScan(state *State, buildLog *BuildLog, depsLog *DepsLog,
	disk_interface util.FileSystem,
	depfile_parser_options *DepfileParserOptions, explanations *Explanations) *DependencyScan {
	return &DependencyScan{
		state:           state,
		buildLog:        buildLog,
		depsLog:         depsLog,
		disk_interface_: disk_interface,
		depLoader:       NewImplicitDepLoader(state, depsLog, disk_interface, depfile_parser_options, explanations),
		dyndepLoader:    NewDyndepLoader(state, disk_interface),
		explanations_:   explanations,
	}
}

func (s *DependencyScan) RecomputeDirty(node *Node, validationNodes *[]*Node) error {
	// 使用栈进行深度优先遍历
	stack := []*Node{node}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if err := s.recomputeNodeDirty(current, &stack, validationNodes); err != nil {
			return err
		}
	}
	return nil
}

func (s *DependencyScan) recomputeNodeDirty(node *Node, stack *[]*Node, validationNodes *[]*Node) error {
	edge := node.InEdge
	if edge == nil {
		if !node.IsExists() {
			node.Dirty = true
		}
		return nil
	}

	if edge.Mark == VisitDone {
		return nil
	}

	if edge.Mark == VisitInStack {
		// 检测循环
		return s.VerifyDAG(node, *stack)
	}

	// 添加验证节点
	*validationNodes = append(*validationNodes, edge.Validations...)

	edge.Mark = VisitInStack
	*stack = append(*stack, node)

	// 加载 dyndep 等（简化）
	if !edge.DepsLoaded {
		edge.DepsLoaded = true
		// ... 加载 depfile 和 dyndep
	}

	// 递归处理输入
	for _, in := range edge.Inputs {
		if err := s.recomputeNodeDirty(in, stack, validationNodes); err != nil {
			return err
		}
	}

	// 脏标记计算（略）
	// ...

	edge.Mark = VisitDone
	*stack = (*stack)[:len(*stack)-1]
	return nil
}

type ImplicitDepLoader struct {
	state                *State
	depsLog              *DepsLog
	diskInterface        util.FileSystem
	depfileParserOptions interface{} // 可忽略
	explanations         interface{}
}

func NewImplicitDepLoader(state *State, depsLog *DepsLog, disk_interface util.FileSystem,
	depfile_parser_options *DepfileParserOptions, explanations *Explanations) *ImplicitDepLoader {
	return &ImplicitDepLoader{
		state:                state,
		depsLog:              depsLog,
		diskInterface:        disk_interface,
		depfileParserOptions: depfile_parser_options,
		explanations:         explanations,
	}
}

func (l *ImplicitDepLoader) LoadDeps(edge *Edge) error {
	// 先尝试从 DepsLog 加载
	if l.depsLog != nil {
		deps := l.depsLog.GetDeps(edge.Outputs[0])
		if deps != nil {
			// 更新边的隐式依赖
			for _, dep := range deps.Nodes {
				edge.Inputs = append(edge.Inputs, dep)
				dep.AddOutEdge(edge)
			}
			edge.DepsLoaded = true
			return nil
		}
	}
	// 否则尝试从 depfile 加载
	depfile := edge.GetUnescapedDepfile()
	if depfile != "" {
		return l.LoadDepFile(edge, depfile)
	}
	return nil
}

func (l *ImplicitDepLoader) LoadDepFile(edge *Edge, path string) error {
	// 读取文件并解析（调用 depfile parser）
	// 这里略，需要集成 DepfileParser
	return nil
}

func (s *DependencyScan) LoadDyndeps(node *Node) error {
	return s.dyndepLoader.LoadDyndeps(node)
}

func (s *DependencyScan) LoadDyndeps2(node *Node) (error, *DyndepFile) {
	return s.dyndepLoader.loadDyndeps(node)
}

func (s *DependencyScan) VerifyDAG(node *Node, stack []*Node) error {
	edge := node.InEdge
	if edge == nil {
		return nil
	}
	if edge.Mark != VisitInStack {
		return nil
	}

	// Find the start of the cycle in the stack
	startIdx := -1
	for i, n := range stack {
		if n.InEdge == edge {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		// Should not happen, but return error
		return fmt.Errorf("internal error: edge not found in stack")
	}

	// Replace the start node with the current node for clearer error message
	stack[startIdx] = node

	// Build error message
	var msg strings.Builder
	msg.WriteString("dependency cycle: ")
	for i := startIdx; i < len(stack); i++ {
		msg.WriteString(stack[i].Path)
		msg.WriteString(" -> ")
	}
	msg.WriteString(stack[startIdx].Path)

	if startIdx+1 == len(stack) && edge.MaybePhonyCycleDiagnostic() {
		msg.WriteString(" [-w phonycycle=err]")
	}

	return fmt.Errorf("%s", msg.String())
}
