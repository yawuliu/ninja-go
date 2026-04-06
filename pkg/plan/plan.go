package plan

import (
	"fmt"
	"ninja-go/pkg/builder"
	"ninja-go/pkg/graph"
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
	builder      *builder.Builder
	want         map[*graph.Edge]Want
	ready        *graph.EdgePriorityQueue
	commandEdges int
	wantedEdges  int
	targets      []*graph.Node
}

func NewPlan(builder *builder.Builder) *Plan {
	return &Plan{
		builder: builder,
		want:    make(map[*graph.Edge]Want),
		ready:   &graph.EdgePriorityQueue{},
	}
}

func (p *Plan) Reset() {
	p.want = make(map[*graph.Edge]Want)
	p.ready.Clear()
	p.commandEdges = 0
	p.wantedEdges = 0
	p.targets = nil
}

func (p *Plan) AddTarget(target *graph.Node) error {
	p.targets = append(p.targets, target)
	return p.addSubTarget(target, nil, nil)
}

func (p *Plan) addSubTarget(node *graph.Node, dependent *graph.Node, dyndepWalk map[*graph.Edge]bool) error {
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

func (p *Plan) edgeWanted(edge *graph.Edge) {
	p.wantedEdges++
	if !edge.IsPhony() {
		p.commandEdges++
		if p.builder != nil && p.builder.status != nil {
			p.builder.status.EdgeAddedToPlan(edge)
		}
	}
}

func (p *Plan) FindWork() *graph.Edge {
	if p.ready.Len() == 0 {
		return nil
	}
	work := p.ready.Top()
	// 若使用jobserver，则尝试获取令牌（此处简化）
	p.ready.Pop()
	return work
}

func (p *Plan) EdgeFinished(edge *graph.Edge, result EdgeResult) error {
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

func (p *Plan) nodeFinished(node *graph.Node) error {
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

func (p *Plan) edgeMaybeReady(edge *graph.Edge, want Want) error {
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

func (p *Plan) scheduleWork(edge *graph.Edge) {
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
	visited := make(map[*graph.Edge]bool)
	sorted := make([]*graph.Edge, 0)
	var dfs func(edge *graph.Edge)
	dfs = func(edge *graph.Edge) {
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
	pools := make(map[*graph.Pool]bool)
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
