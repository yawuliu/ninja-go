package main

import (
	"errors"
	"fmt"
)

type Want int

const (
	kWantNothing Want = iota
	kWantToStart
	kWantToFinish
) // Plan 构建计划

type EdgeResult int

const (
	kEdgeFailed EdgeResult = iota
	kEdgeSucceeded
)

type Plan struct {
	builder_       *Builder
	want_          map[*Edge]Want
	ready_         *EdgePriorityQueue
	command_edges_ int
	wanted_edges_  int
	targets_       []*Node
}

func NewPlan(builder *Builder) *Plan {
	return &Plan{
		builder_:       builder,
		want_:          make(map[*Edge]Want),
		ready_:         &EdgePriorityQueue{},
		command_edges_: 0,
		wanted_edges_:  0,
	}
}

func (p *Plan) Reset() {
	p.want_ = make(map[*Edge]Want)
	p.ready_.Clear()
	p.command_edges_ = 0
	p.wanted_edges_ = 0
}

func (p *Plan) AddTarget(target *Node, err *string) bool {
	p.targets_ = append(p.targets_, target)
	return p.addSubTarget(target, nil, err, nil)
}

func (p *Plan) addSubTarget(node *Node, dependent *Node, err *string, dyndep_walk map[*Edge]bool) bool {
	edge := node.in_edge()
	if edge == nil {
		// 叶子节点：若是源文件且缺失且不是由dep loader生成，则报错
		if node.dirty_ && !node.generated_by_dep_loader_ {
			var ref string
			if dependent != nil {
				ref = ", needed by '" + dependent.path_ + "',"
			}
			*err = fmt.Sprintf("'%s'%s missing and no known rule to make it", node.path_, ref)
		}
		return false
	}

	if edge.outputs_ready_ {
		return false
	}

	want, exists := p.want_[edge]
	if !exists {
		want = kWantNothing
		p.want_[edge] = want
	}

	if dyndep_walk != nil && want == kWantToFinish {
		return false
	}

	if node.dirty_ && want == kWantNothing {
		p.want_[edge] = kWantToStart
		p.edgeWanted(edge)
	}

	if dyndep_walk != nil {
		dyndep_walk[edge] = true
	}

	if exists {
		return true
	}

	for _, in := range edge.inputs_ {
		if !p.addSubTarget(in, node, err, dyndep_walk) && *err != "" {
			return false
		}
	}
	return true
}

func (p *Plan) edgeWanted(edge *Edge) {
	p.wanted_edges_++
	if !edge.IsPhony() {
		p.command_edges_++
		if p.builder_ != nil {
			p.builder_.status_.EdgeAddedToPlan(edge)
		}
	}
}

func (p *Plan) FindWork() *Edge {
	if p.ready_.Len() == 0 {
		return nil
	}
	work := p.ready_.Top()
	// If jobserver mode is enabled, try to acquire a token first,
	// and return null in case of failure.
	if p.builder_ != nil && p.builder_.jobserver_ != nil {
		work.job_slot_ = p.builder_.jobserver_.TryAcquire()
		if !work.job_slot_.IsValid() {
			return nil
		}
	}

	p.ready_.Pop()
	return work
}

func (p *Plan) EdgeFinished(edge *Edge, result EdgeResult, err *string) bool {
	e, exists := p.want_[edge]
	if !exists {
		panic(errors.New("EdgeFinished"))
	}
	directlyWanted := e != kWantNothing

	if directlyWanted {
		edge.pool_.EdgeFinished(edge)
	}
	edge.pool_.RetrieveReadyEdges(p.ready_)

	if result != kEdgeSucceeded {
		return true
	}

	if directlyWanted {
		p.wanted_edges_--
	}
	delete(p.want_, edge)
	edge.outputs_ready_ = true

	for _, out := range edge.outputs_ {
		if !p.nodeFinished(out, err) {
			return false
		}
	}
	return true
}

func (p *Plan) nodeFinished(node *Node, err *string) bool {
	// 若此节点提供 dyndep 信息，则加载
	if node.dyndep_pending_ {
		if p.builder_ == nil {
			panic(fmt.Errorf("dyndep requires Plan to have a Builder"))
		}
		return p.builder_.LoadDyndeps(node, err)
	}

	for _, outEdge := range node.out_edges_ {
		want, exists := p.want_[outEdge]
		if !exists {
			continue
		}
		if !p.EdgeMaybeReady(p.want_, outEdge, want, err) {
			return false
		}
	}
	return true
}

func (p *Plan) EdgeMaybeReady(target map[*Edge]Want, want_e_first *Edge, want_e_second Want, err *string) bool {
	edge := want_e_first
	if edge.AllInputsReady() {
		if want_e_second != kWantNothing {
			p.ScheduleWork(target, want_e_first, want_e_second)
		} else {
			if !p.EdgeFinished(edge, kEdgeSucceeded, err) {
				return false
			}
		}
	}
	return true
}

func (p *Plan) ScheduleWork(target map[*Edge]Want, want_e_first *Edge, want_e_second Want) {
	if want_e_second == kWantToFinish {
		// This edge has already been scheduled.  We can get here again if an edge
		// and one of its dependencies share an order-only input, or if a node
		// duplicates an out edge (see https://github.com/ninja-build/ninja/pull/519).
		// Avoid scheduling the work again.
		return
	}
	if want_e_second != kWantToStart {
		panic(fmt.Sprintf("ScheduleWork want_e_second=%v", want_e_second))
	}
	target[want_e_first] = kWantToFinish

	edge := want_e_first
	pool := edge.pool_
	if pool.ShouldDelayEdge() {
		pool.DelayEdge(edge)
		pool.RetrieveReadyEdges(p.ready_)
	} else {
		pool.EdgeScheduled(edge)
		p.ready_.Push(edge)
	}
}

func (p *Plan) PrepareQueue() {
	p.computeCriticalPath()
	p.ScheduleInitialEdges()
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
		for _, in := range edge.inputs_ {
			if prod := in.in_edge(); prod != nil {
				dfs(prod)
			}
		}
		sorted = append(sorted, edge)
	}
	for _, target := range p.targets_ {
		if edge := target.in_edge(); edge != nil {
			dfs(edge)
		}
	}
	// 初始化权重
	for _, e := range sorted {
		weight := 1
		if e.IsPhony() {
			weight = 0
		}
		e.critical_path_weight_ = int64(weight)
	}
	// 反向传播
	for i := len(sorted) - 1; i >= 0; i-- {
		e := sorted[i]
		for _, in := range e.inputs_ {
			if prod := in.in_edge(); prod != nil {
				cand := e.critical_path_weight_ + 1
				if prod.IsPhony() {
					cand = e.critical_path_weight_
				}
				if cand > prod.critical_path_weight_ {
					prod.critical_path_weight_ = cand
				}
			}
		}
	}
}

func (p *Plan) ScheduleInitialEdges() {
	pools := make(map[*Pool]bool)
	for edge, want := range p.want_ {
		if want == kWantToStart && edge.AllInputsReady() {
			pool := edge.pool_
			if pool.ShouldDelayEdge() {
				pool.DelayEdge(edge)
				pools[pool] = true
			} else {
				p.ScheduleWork(p.want_, edge, want)
			}
		}
	}
	for pool := range pools {
		pool.RetrieveReadyEdges(p.ready_)
	}
}

func (p *Plan) MoreToDo() bool {
	return p.wanted_edges_ > 0 && p.command_edges_ > 0
}

func (p *Plan) CommandEdgeCount() int {
	return p.command_edges_
}

// CleanNode 将节点标记为 clean，并递归清理所有依赖该节点的边（如果这些边不再 dirty）。
func (p *Plan) CleanNode(scan *DependencyScan, node *Node, err *string) bool {
	node.dirty_ = false

	for _, outEdge := range node.out_edges_ {
		// 忽略不在计划中的边，或者已经被标记为不想要的边
		want, exists := p.want_[outEdge]
		if !exists || want == kWantNothing {
			continue
		}

		// 如果该边之前加载依赖失败，则不处理
		if outEdge.deps_missing_ {
			continue
		}

		// 检查所有非 order-only 输入是否都已 clean
		begin := 0
		end := len(outEdge.inputs_) - outEdge.order_only_deps_
		allClean := true
		for i := begin; i < end; i++ {
			if outEdge.inputs_[i].dirty_ {
				allClean = false
				break
			}
		}

		if allClean {
			// 重新计算最新的输入 mtime
			var mostRecentInput *Node
			for i := begin; i < end; i++ {
				if mostRecentInput == nil || outEdge.inputs_[i].mtime_ > mostRecentInput.mtime_ {
					mostRecentInput = outEdge.inputs_[i]
				}
			}

			// 判断该边的输出是否 dirty
			var outputsDirty bool = false
			if !scan.RecomputeOutputsDirty(outEdge, mostRecentInput, &outputsDirty, err) {
				return false
			}

			if !outputsDirty {
				// 递归清理该边的所有输出节点
				for _, out := range outEdge.outputs_ {
					if !p.CleanNode(scan, out, err) {
						return false
					}
				}

				// 将该边从计划中移除
				p.want_[outEdge] = kWantNothing
				p.wanted_edges_--
				if !outEdge.IsPhony() {
					p.command_edges_--
					if p.builder_ != nil && p.builder_.status_ != nil {
						p.builder_.status_.EdgeRemovedFromPlan(outEdge)
					}
				}
			}
		}
	}
	return true
}

// DyndepsLoaded 在加载 dyndep 文件后更新计划，将新发现的边加入构建图。
func (p *Plan) DyndepsLoaded(scan *DependencyScan, node *Node, ddf DyndepFile, err *string) bool {
	// 重新计算所有直接和间接依赖的脏状态
	if !p.RefreshDyndepDependents(scan, node, err) {
		return false
	}

	// 收集已经在计划中且有新 dyndep 信息的边
	var dyndepRoots []*Edge
	for edge := range ddf {
		if edge.outputs_ready_ {
			continue
		}
		if _, ok := p.want_[edge]; !ok {
			continue
		}
		dyndepRoots = append(dyndepRoots, edge)
	}

	// 通过新发现的隐式输入，遍历图中尚未加入计划的部分
	dyndepWalk := make(map[*Edge]bool)
	for _, edge := range dyndepRoots {
		info := ddf[edge]
		for _, in := range info.ImplicitInputs {
			// AddSubTarget 的第三个参数是 dependent，这里用 edge_ 的第一个输出
			var dependentNode *Node
			if len(edge.outputs_) > 0 {
				dependentNode = edge.outputs_[0]
			}
			if !p.addSubTarget(in, dependentNode, err, dyndepWalk) && *err != "" {
				return false
			}
		}
	}

	// 添加该节点的出边（原本应在 NodeFinished 中处理）
	for _, outEdge := range node.out_edges_ {
		if _, ok := p.want_[outEdge]; ok {
			dyndepWalk[outEdge] = true
		}
	}

	// 检查这些边是否就绪
	for edge := range dyndepWalk {
		want, ok := p.want_[edge]
		if !ok {
			continue
		}
		if !p.EdgeMaybeReady(p.want_, edge, want, err) {
			return false
		}
	}

	return true
}

// RefreshDyndepDependents 重新计算依赖节点的脏状态，并根据需要将它们加入计划。
func (p *Plan) RefreshDyndepDependents(scan *DependencyScan, node *Node, err *string) bool {
	// 收集依赖节点的传递闭包，并标记它们的边为未访问
	dependents := make(map[*Node]bool)
	p.unmarkDependents(node, dependents)

	// 更新所有依赖的脏状态，并检查它们的边是否变为想要
	for n := range dependents {
		// 重新计算脏状态，同时检测新循环
		var validationNodes []*Node
		if !scan.RecomputeDirty(n, &validationNodes, err) {
			return false
		}

		// 将新发现的验证节点添加为顶层目标
		for _, vn := range validationNodes {
			if inEdge := vn.in_edge(); inEdge != nil {
				if !inEdge.outputs_ready_ && !p.AddTarget(vn, err) {
					return false
				}
			}
		}

		if !n.dirty_ {
			continue
		}

		// 该边之前遇到过，但可能由于输出不脏而不想构建。现在有了 dyndep 信息，输出变脏，需要构建。
		edge := n.in_edge()
		if edge == nil || edge.outputs_ready_ {
			continue
		}
		want, ok := p.want_[edge]
		if !ok {
			continue
		}
		if want == kWantNothing {
			p.want_[edge] = kWantToStart
			p.edgeWanted(edge)
		}
	}
	return true
}

// unmarkDependents 递归地清除节点依赖边的访问标记，并将所有依赖节点添加到 dependents 集合中。
func (p *Plan) unmarkDependents(node *Node, dependents map[*Node]bool) {
	for _, outEdge := range node.out_edges_ {
		// 如果该边不在计划中，跳过
		if _, ok := p.want_[outEdge]; !ok {
			continue
		}
		// 如果边尚未标记为已访问，则将其标记清除并递归处理其输出节点
		if outEdge.mark_ != VisitNone {
			outEdge.mark_ = VisitNone
			for _, out := range outEdge.outputs_ {
				if _, ok := dependents[out]; !ok {
					dependents[out] = true
					p.unmarkDependents(out, dependents)
				}
			}
		}
	}
}
