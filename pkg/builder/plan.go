package builder

import (
	"fmt"
)

type Want int

const (
	WantNothing Want = iota
	WantToStart
	WantToFinish
) // Plan 构建计划

type EdgeResult int

const (
	EdgeFailed EdgeResult = iota
	EdgeSucceeded
)

type Plan struct {
	builder      *Builder
	want         map[*Edge]Want
	ready        *EdgePriorityQueue
	commandEdges int
	wantedEdges  int
	targets      []*Node
}

func NewPlan(builder *Builder) *Plan {
	return &Plan{
		builder: builder,
		want:    make(map[*Edge]Want),
		ready:   &EdgePriorityQueue{},
	}
}

func (p *Plan) Reset() {
	p.want = make(map[*Edge]Want)
	p.ready.Clear()
	p.commandEdges = 0
	p.wantedEdges = 0
	p.targets = nil
}

func (p *Plan) AddTarget(target *Node) error {
	p.targets = append(p.targets, target)
	return p.addSubTarget(target, nil, nil)
}

func (p *Plan) addSubTarget(node *Node, dependent *Node, dyndepWalk map[*Edge]bool) error {
	edge := node.InEdge
	if edge == nil {
		// 叶子节点：若是源文件且缺失且不是由dep loader生成，则报错
		if node.Dirty && !node.GeneratedByDepLoader {
			var ref string
			if dependent != nil {
				ref = ", needed by '" + dependent.Path + "',"
			}
			return fmt.Errorf("'%s'%s missing and no known rule to make it", node.Path, ref)
		}
		return nil
	}

	if edge.OutputsReady {
		return nil
	}

	want, exists := p.want[edge]
	if !exists {
		want = WantNothing
		p.want[edge] = want
	}

	if dyndepWalk != nil && want == WantToFinish {
		return nil
	}

	if node.Dirty && want == WantNothing {
		p.want[edge] = WantToStart
		p.edgeWanted(edge)
	}

	if dyndepWalk != nil {
		dyndepWalk[edge] = true
	}

	if exists {
		return nil
	}

	for _, in := range edge.Inputs {
		if err := p.addSubTarget(in, node, dyndepWalk); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plan) edgeWanted(edge *Edge) {
	p.wantedEdges++
	if !edge.IsPhony() {
		p.commandEdges++
		if p.builder != nil && p.builder.status != nil {
			p.builder.status.EdgeAddedToPlan(edge)
		}
	}
}

func (p *Plan) FindWork() *Edge {
	if p.ready.Len() == 0 {
		return nil
	}
	work := p.ready.Top()
	// 若使用jobserver，则尝试获取令牌（此处简化）
	p.ready.Pop()
	return work
}

func (p *Plan) EdgeFinished(edge *Edge, result EdgeResult) error {
	e, exists := p.want[edge]
	if !exists {
		return nil
	}
	directlyWanted := e != WantNothing

	if directlyWanted {
		edge.Pool.EdgeFinished(edge)
	}
	edge.Pool.RetrieveReadyEdges(p.ready)

	if result != EdgeSucceeded {
		return nil
	}

	if directlyWanted {
		p.wantedEdges--
	}
	delete(p.want, edge)
	edge.OutputsReady = true

	for _, out := range edge.Outputs {
		if err := p.nodeFinished(out); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plan) nodeFinished(node *Node) error {
	// 若此节点提供 dyndep 信息，则加载
	if node.DyndepPending {
		if p.builder == nil {
			return fmt.Errorf("dyndep requires Plan to have a Builder")
		}
		return p.builder.LoadDyndeps(node)
	}

	for _, outEdge := range node.OutEdges {
		want, exists := p.want[outEdge]
		if !exists {
			continue
		}
		if err := p.edgeMaybeReady(outEdge, want); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plan) edgeMaybeReady(edge *Edge, want Want) error {
	if edge.AllInputsReady() {
		if want != WantNothing {
			p.scheduleWork(edge)
		} else {
			if err := p.EdgeFinished(edge, EdgeSucceeded); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Plan) scheduleWork(edge *Edge) {
	e, exists := p.want[edge]
	if !exists || e != WantToStart {
		return
	}
	p.want[edge] = WantToFinish
	pool := edge.Pool
	if pool.ShouldDelayEdge() {
		pool.DelayEdge(edge)
		pool.RetrieveReadyEdges(p.ready)
	} else {
		pool.EdgeScheduled(edge)
		p.ready.Push(edge)
	}
}

func (p *Plan) PrepareQueue() {
	p.computeCriticalPath()
	p.scheduleInitialEdges()
}

func (p *Plan) computeCriticalPath() {
	// 拓扑排序
	visited := make(map[*Edge]bool)
	sorted := make([]*Edge, 0)
	var dfs func(edge *Edge)
	dfs = func(edge *Edge) {
		if visited[edge] {
			return
		}
		visited[edge] = true
		for _, in := range edge.Inputs {
			if prod := in.InEdge; prod != nil {
				dfs(prod)
			}
		}
		sorted = append(sorted, edge)
	}
	for _, target := range p.targets {
		if edge := target.InEdge; edge != nil {
			dfs(edge)
		}
	}
	// 初始化权重
	for _, e := range sorted {
		weight := 1
		if e.IsPhony() {
			weight = 0
		}
		e.CriticalPathWeight = int64(weight)
	}
	// 反向传播
	for i := len(sorted) - 1; i >= 0; i-- {
		e := sorted[i]
		for _, in := range e.Inputs {
			if prod := in.InEdge; prod != nil {
				cand := e.CriticalPathWeight + 1
				if prod.IsPhony() {
					cand = e.CriticalPathWeight
				}
				if cand > prod.CriticalPathWeight {
					prod.CriticalPathWeight = cand
				}
			}
		}
	}
}

func (p *Plan) scheduleInitialEdges() {
	p.ready.Clear()
	pools := make(map[*Pool]bool)
	for edge, want := range p.want {
		if want == WantToStart && edge.AllInputsReady() {
			pool := edge.Pool
			if pool.ShouldDelayEdge() {
				pool.DelayEdge(edge)
				pools[pool] = true
			} else {
				p.scheduleWork(edge)
			}
		}
	}
	for pool := range pools {
		pool.RetrieveReadyEdges(p.ready)
	}
}

func (p *Plan) MoreToDo() bool {
	return p.wantedEdges > 0 && p.commandEdges > 0
}

func (p *Plan) CommandEdgeCount() int {
	return p.commandEdges
}

// CleanNode 将节点标记为 clean，并递归清理所有依赖该节点的边（如果这些边不再 dirty）。
func (p *Plan) CleanNode(scan *DependencyScan, node *Node) error {
	node.Dirty = false

	for _, outEdge := range node.OutEdges {
		// 忽略不在计划中的边，或者已经被标记为不想要的边
		want, exists := p.want[outEdge]
		if !exists || want == WantNothing {
			continue
		}

		// 如果该边之前加载依赖失败，则不处理
		if outEdge.DepsMissing {
			continue
		}

		// 检查所有非 order-only 输入是否都已 clean
		begin := 0
		end := len(outEdge.Inputs) - outEdge.OrderOnlyDeps
		allClean := true
		for i := begin; i < end; i++ {
			if outEdge.Inputs[i].Dirty {
				allClean = false
				break
			}
		}

		if allClean {
			// 重新计算最新的输入 mtime
			var mostRecentInput *Node
			for i := begin; i < end; i++ {
				if mostRecentInput == nil || outEdge.Inputs[i].Mtime > mostRecentInput.Mtime {
					mostRecentInput = outEdge.Inputs[i]
				}
			}

			// 判断该边的输出是否 dirty
			outputsDirty, err := scan.RecomputeOutputsDirty(outEdge, mostRecentInput)
			if err != nil {
				return err
			}

			if !outputsDirty {
				// 递归清理该边的所有输出节点
				for _, out := range outEdge.Outputs {
					if err := p.CleanNode(scan, out); err != nil {
						return err
					}
				}

				// 将该边从计划中移除
				p.want[outEdge] = WantNothing
				p.wantedEdges--
				if !outEdge.IsPhony() {
					p.commandEdges--
					if p.builder != nil && p.builder.status != nil {
						p.builder.status.EdgeRemovedFromPlan(outEdge)
					}
				}
			}
		}
	}
	return nil
}

// DyndepsLoaded 在加载 dyndep 文件后更新计划，将新发现的边加入构建图。
func (p *Plan) DyndepsLoaded(scan *DependencyScan, node *Node, ddf DyndepFile) error {
	// 重新计算所有直接和间接依赖的脏状态
	if err := p.RefreshDyndepDependents(scan, node); err != nil {
		return err
	}

	// 收集已经在计划中且有新 dyndep 信息的边
	var dyndepRoots []*Edge
	for edge := range ddf {
		if edge.OutputsReady {
			continue
		}
		if _, ok := p.want[edge]; !ok {
			continue
		}
		dyndepRoots = append(dyndepRoots, edge)
	}

	// 通过新发现的隐式输入，遍历图中尚未加入计划的部分
	dyndepWalk := make(map[*Edge]bool)
	for _, edge := range dyndepRoots {
		info := ddf[edge]
		for _, in := range info.ImplicitInputs {
			// AddSubTarget 的第三个参数是 dependent，这里用 edge 的第一个输出
			var dependentNode *Node
			if len(edge.Outputs) > 0 {
				dependentNode = edge.Outputs[0]
			}
			if err := p.addSubTarget(in, dependentNode, dyndepWalk); err != nil {
				return err
			}
		}
	}

	// 添加该节点的出边（原本应在 NodeFinished 中处理）
	for _, outEdge := range node.OutEdges {
		if _, ok := p.want[outEdge]; ok {
			dyndepWalk[outEdge] = true
		}
	}

	// 检查这些边是否就绪
	for edge := range dyndepWalk {
		want, ok := p.want[edge]
		if !ok {
			continue
		}
		if err := p.edgeMaybeReady(edge, want); err != nil {
			return err
		}
	}

	return nil
}

// RefreshDyndepDependents 重新计算依赖节点的脏状态，并根据需要将它们加入计划。
func (p *Plan) RefreshDyndepDependents(scan *DependencyScan, node *Node) error {
	// 收集依赖节点的传递闭包，并标记它们的边为未访问
	dependents := make(map[*Node]bool)
	p.unmarkDependents(node, dependents)

	// 更新所有依赖的脏状态，并检查它们的边是否变为想要
	for n := range dependents {
		// 重新计算脏状态，同时检测新循环
		var validationNodes []*Node
		if err := scan.RecomputeDirty(n, &validationNodes); err != nil {
			return err
		}

		// 将新发现的验证节点添加为顶层目标
		for _, vn := range validationNodes {
			if inEdge := vn.InEdge; inEdge != nil {
				if !inEdge.OutputsReady {
					if err := p.AddTarget(vn); err != nil {
						return err
					}
				}
			}
		}

		if !n.Dirty {
			continue
		}

		// 该边之前遇到过，但可能由于输出不脏而不想构建。现在有了 dyndep 信息，输出变脏，需要构建。
		edge := n.InEdge
		if edge == nil || edge.OutputsReady {
			continue
		}
		want, ok := p.want[edge]
		if !ok {
			continue
		}
		if want == WantNothing {
			p.want[edge] = WantToStart
			p.edgeWanted(edge)
		}
	}
	return nil
}

// unmarkDependents 递归地清除节点依赖边的访问标记，并将所有依赖节点添加到 dependents 集合中。
func (p *Plan) unmarkDependents(node *Node, dependents map[*Node]bool) {
	for _, outEdge := range node.OutEdges {
		// 如果该边不在计划中，跳过
		if _, ok := p.want[outEdge]; !ok {
			continue
		}
		// 如果边尚未标记为已访问，则将其标记清除并递归处理其输出节点
		if outEdge.Mark != VisitNone {
			outEdge.Mark = VisitNone
			for _, out := range outEdge.Outputs {
				if _, ok := dependents[out]; !ok {
					dependents[out] = true
					p.unmarkDependents(out, dependents)
				}
			}
		}
	}
}
