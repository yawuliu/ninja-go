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
	depfile_parser_options *DepfileParserOptions, explanations *OptionalExplanations) *DependencyScan {
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

func (s *DependencyScan) RecomputeDirty(node *Node, validationNodes *[]*Node) (bool, error) {
	// 使用栈进行深度优先遍历
	stack := []*Node{node}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if succ, err := s.recomputeNodeDirty(current, &stack, validationNodes); !succ && err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *DependencyScan) recomputeNodeDirty(node *Node, stack *[]*Node, validationNodes *[]*Node) (bool, error) {
	edge := node.InEdge
	if edge == nil {
		if !node.IsExists() {
			node.Dirty = true
		}
		return true, nil
	}

	if edge.Mark == VisitDone {
		return true, nil
	}

	if err := s.VerifyDAG(node, *stack); err != nil {
		// 检测循环
		return false, err
	}

	// 添加验证节点
	*validationNodes = append(*validationNodes, edge.Validations...)

	edge.Mark = VisitInStack
	*stack = append(*stack, node)

	dirty := false
	edge.OutputsReady = true
	edge.DepsMissing = false
	// 加载 dyndep 等（简化）
	if !edge.DepsLoaded {
		// This is our first encounter with this edge.
		edge.DepsLoaded = true
		// If there is a pending dyndep file, visit it now:
		// * If the dyndep file is ready then load it now to get any
		//   additional inputs and outputs for this and other edges.
		//   Once the dyndep file is loaded it will no longer be pending
		//   if any other edges encounter it, but they will already have
		//   been updated.
		// * If the dyndep file is not ready then since is known to be an
		//   input to this edge, the edge will not be considered ready below.
		//   Later during the build the dyndep file will become ready and be
		//   loaded to update this edge before it can possibly be scheduled.
		if edge.DyndepFile!=nil && edge.DyndepFile.DyndepPending {
			if (!s.RecomputeNodeDirty(edge.DyndepFile, stack, validationNodes)) {
				return false
			}
			if !edge->dyndep_->in_edge() || edge->dyndep_->in_edge()->outputs_ready() {
				// The dyndep file is ready, so load it now.
				if !s.LoadDyndeps(edge->dyndep_) {
					return false
				}
			}
		}
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
	depfile_parser_options *DepfileParserOptions, explanations *OptionalExplanations) *ImplicitDepLoader {
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

func (s *DependencyScan) LoadDyndeps2(node *Node, ddf *DyndepFile) error {
	return s.dyndepLoader.loadDyndeps(node, ddf)
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

func (s *DependencyScan) RecomputeOutputsDirty(edge *Edge, mostRecentInput *Node) (bool, error) {
	command := edge.EvaluateCommand(true)
	for _, out := range edge.Outputs {
		dirty, err := s.RecomputeOutputDirty(edge, mostRecentInput, command, out)
		if err != nil {
			return false, err
		}
		if dirty {
			return true, nil
		}
	}
	return false, nil
}

// RecomputeOutputDirty 判断单个输出节点是否需要重新构建（是否脏）。
// 参数 edge 是产生该输出的边，mostRecentInput 是最近修改的输入节点，
// command 是边的完整命令（用于比较命令哈希），output 是输出节点。
// 返回 true 表示需要重新构建，false 表示 clean。
func (s *DependencyScan) RecomputeOutputDirty(edge *Edge, mostRecentInput *Node, command string, output *Node) (bool, error) {
	// 处理 phony 边
	if edge.IsPhony() {
		// phony 边不写入任何输出。只有当没有输入、没有验证边且输出文件不存在时，才标记为脏。
		if len(edge.Inputs) == 0 && len(edge.Validations) == 0 && !output.IsExists() {
			s.explanations_.Record(output, "output %s of phony edge with no inputs doesn't exist", output.Path)
			return true, nil
		}
		// 用最新输入的时间戳更新 phony 节点的 mtime（供下游使用）
		if mostRecentInput != nil {
			output.UpdatePhonyMtime(mostRecentInput.Mtime)
		}
		return false, nil
	}

	// 如果输出文件不存在，标记为脏
	if !output.IsExists() {
		s.explanations_.Record(output, "output %s doesn't exist", output.Path)
		return true, nil
	}

	// 如果是 restat 规则，可能之前已经清理过输出，并且将命令开始时间记录在日志中。
	// restat 规则不检查输出的实际 mtime，而是比较日志中的记录 mtime 与最新输入的 mtime。
	var entry *LogEntry
	usedRestat := false
	if edge.GetBindingBool("restat") && s.buildLog != nil {
		entry = s.buildLog.LookupByOutput(output.Path)
		if entry != nil {
			usedRestat = true
		}
	}

	// 如果不是 restat 规则，且输出比最新输入旧，则标记脏
	if !usedRestat && mostRecentInput != nil && output.Mtime < mostRecentInput.Mtime {
		s.explanations_.Record(output,
			"output %s older than most recent input %s (%d vs %d)",
			output.Path, mostRecentInput.Path, output.Mtime, mostRecentInput.Mtime)
		return true, nil
	}

	// 使用 build log 进行进一步检查
	if s.buildLog != nil {
		generator := edge.GetBindingBool("generator")
		if entry == nil {
			entry = s.buildLog.LookupByOutput(output.Path)
		}
		if entry != nil {
			// 如果命令哈希发生变化（且不是 generator 规则），则标记脏
			if !generator && HashCommand(command) != entry.CommandHash {
				s.explanations_.Record(output, "command line changed for %s", output.Path)
				return true, nil
			}
			// 如果日志中的 mtime 比最新输入旧，则标记脏（即使磁盘上的 mtime 更新）
			if mostRecentInput != nil && entry.Mtime < mostRecentInput.Mtime {
				s.explanations_.Record(output,
					"recorded mtime of %s older than most recent input %s (%d vs %d)",
					output.Path, mostRecentInput.Path, entry.Mtime, mostRecentInput.Mtime)
				return true, nil
			}
		} else if !generator {
			// 没有日志记录且不是 generator 规则，则标记脏
			s.explanations_.Record(output, "command line not found in log for %s", output.Path)
			return true, nil
		}
	}

	return false, nil
}
