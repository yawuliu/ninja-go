package plan

import (
	"errors"
	"ninja-go/pkg/buildlog"
	"ninja-go/pkg/graph"
	"ninja-go/pkg/util"
)

type Plan struct {
	state    *graph.State
	queue    []*graph.Edge // 就绪队列
	buildLog *buildlog.BuildLog
}

func NewPlan(state *graph.State, bl *buildlog.BuildLog) *Plan {
	return &Plan{state: state, buildLog: bl}
}
func (p *Plan) visit(fs util.FileSystem, n *graph.Node, need map[*graph.Node]bool) error {
	if need[n] {
		return nil
	}
	need[n] = true
	if n.Edge != nil {
		// 检查输入是否脏
		for _, in := range n.Edge.Inputs {
			if err := p.visit(fs, in, need); err != nil {
				return err
			}
		}
		for _, imp := range n.Edge.ImplicitDeps {
			if err := p.visit(fs, imp, need); err != nil {
				return err
			}
		}
		// order-only 依赖不触发脏标记，但需要存在
		for _, ord := range n.Edge.OrderOnlyDeps {
			if err := ord.LoadMtime(fs); err != nil {
				return err
			}
			if ord.Mtime == -1 {
				return errors.New("missing order-only dependency: " + ord.Path)
			}
			if err := p.visit(fs, ord, need); err != nil {
				return err
			}
		}
		if n.Edge.DyndepFile != nil {
			if err := p.visit(fs, n.Edge.DyndepFile, need); err != nil {
				return err
			}
		}
	}
	return nil
}

// Compute 从目标节点开始标记脏节点，并返回拓扑顺序的边列表
func (p *Plan) Compute(fs util.FileSystem, targets []*graph.Node) ([]*graph.Edge, error) {
	// 1. 收集所有需要构建的节点（后序遍历）
	need := make(map[*graph.Node]bool)
	for _, t := range targets {
		if err := p.visit(fs, t, need); err != nil {
			return nil, err
		}
	}

	// 2. 脏标记：比较输出和输入的时间戳
	// 脏标记：比较时间戳或命令哈希
	for n := range need {
		if err := n.LoadMtime(fs); err != nil {
			return nil, err
		}
		if n.Edge != nil {
			// 如果输出文件不存在，直接标记为脏
			if n.Mtime == -1 {
				n.Dirty = true
				continue
			}
			// 检查输出是否比任何输入旧
			var newestInput int64 = -1
			allInputs := append([]*graph.Node{}, n.Edge.Inputs...)
			// 隐式依赖也要检查
			allInputs = append(allInputs, n.Edge.ImplicitDeps...)
			for _, in := range allInputs {
				if err := in.LoadMtime(fs); err != nil {
					return nil, err
				}
				//if in.Mtime == -1 && !in.Generated {
				//	// 输入文件不存在且不是由其他规则生成，需要尝试构建它
				//	// 如果 in 本身有对应的 edge，则会被递归处理；否则报错
				//	if in.Edge == nil {
				//		return nil, fmt.Errorf("missing input file: %s", in.Path)
				//	}
				//	// 标记当前边为 dirty
				//	n.Dirty = true
				//}
				if in.Mtime > newestInput {
					newestInput = in.Mtime
				}
			}
			//for _, imp := range n.Edge.ImplicitDeps {
			//	if err := imp.LoadMtime(); err != nil {
			//		return nil, err
			//	}
			//	if imp.Mtime > newestInput {
			//		newestInput = imp.Mtime
			//	}
			//}
			if n.Mtime < newestInput || n.Mtime == -1 {
				n.Dirty = true
			} else {
				// 检查命令哈希是否改变
				lastHash := p.buildLog.GetCommandHash(n.Path)
				if lastHash == "" {
					n.Dirty = true
				} else {
					currentHash := graph.ComputeCommandHash(n.Edge) // 需要实现此函数
					if lastHash != currentHash {
						n.Dirty = true
					}
				}
			}
			// 如果有 depfile，稍后处理
		}
	}

	// 3. 收集需要构建的边（输出脏的边）
	edgesSet := make(map[*graph.Edge]bool)
	for n := range need {
		if n.Dirty && n.Edge != nil {
			edgesSet[n.Edge] = true
		}
	}
	edges := make([]*graph.Edge, 0, len(edgesSet))
	for e := range edgesSet {
		edges = append(edges, e)
	}

	return topologicalSort(edges)
}

func topologicalSort(edges []*graph.Edge) ([]*graph.Edge, error) {
	// 4. 拓扑排序（简单按依赖深度排序）
	// 计算每个边的入度（依赖的边）
	inDegree := make(map[*graph.Edge]int)
	for _, e := range edges {
		for _, out := range e.Outputs {
			for _, depEdge := range edges {
				for _, in := range depEdge.Inputs {
					if in == out {
						inDegree[depEdge]++
					}
				}
			}
		}
	}
	// Kahn 算法
	var sorted []*graph.Edge
	queue := []*graph.Edge{}
	for _, e := range edges {
		if inDegree[e] == 0 {
			queue = append(queue, e)
		}
	}
	for len(queue) > 0 {
		e := queue[0]
		queue = queue[1:]
		sorted = append(sorted, e)
		// 减少依赖该边的边的入度
		for _, out := range e.Outputs {
			for _, depEdge := range edges {
				for _, in := range depEdge.Inputs {
					if in == out {
						inDegree[depEdge]--
						if inDegree[depEdge] == 0 {
							queue = append(queue, depEdge)
						}
					}
				}
			}
		}
	}
	if len(sorted) != len(edges) {
		return nil, errors.New("circular dependency detected")
	}
	return sorted, nil
}
