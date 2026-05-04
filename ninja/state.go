package main

import (
	"fmt"
)

var kDefaultPool *Pool = NewPool("", 0)
var kConsolePool *Pool = NewPool("console", 1)

type State struct {
	paths_    map[string]*Node
	pools_    map[string]*Pool
	Rules     map[string]*Rule
	edges_    []*Edge
	bindings_ *BindingEnv
	defaults_ []*Node // default 语句指定的目标
}

func NewState() *State {
	s := &State{
		paths_:    make(map[string]*Node),
		pools_:    make(map[string]*Pool),
		edges_:    []*Edge{},
		Rules:     make(map[string]*Rule),
		bindings_: NewBindingEnv(nil),
		defaults_: []*Node{},
	}
	s.bindings_.AddRule(PhonyRule())
	s.AddPool(kDefaultPool)
	s.AddPool(kConsolePool)
	return s
}

func (s *State) AddPool(pool *Pool) {
	if s.LookupPool(pool.Name) != nil {
		panic("pool_ already defined")
	}
	s.pools_[pool.Name] = pool
}

func (s *State) LookupPool(name string) *Pool {
	if _, ok := s.pools_[name]; ok {
		return s.pools_[name]
	}
	return nil
}

func (s *State) AddEdge(rule *Rule) *Edge {
	edge := &Edge{
		rule_: rule,
		pool_: kDefaultPool,
		env_:  s.bindings_,
		id_:   uint64(len(s.edges_)),
	}
	s.edges_ = append(s.edges_, edge)
	return edge
}

// GetNode 返回指定路径的节点，如果不存在则创建（与 AddNode 相同，但语义更清晰）
func (s *State) GetNode(path string, slash_bits uint64) *Node {
	node := s.LookupNode(path)
	if node != nil {
		return node
	}
	node = NewNode(path, slash_bits)
	s.paths_[node.path_] = node
	return node
}

func (s *State) LookupNode(path string) *Node {
	_, ok := s.paths_[path]
	if ok {
		return s.paths_[path]
	}
	return nil
}

func (s *State) SpellcheckNode(path string) *Node {
	const allowReplacements = true
	const maxValidEditDistance = 3

	minDistance := maxValidEditDistance + 1
	var result *Node

	for p, node := range s.paths_ {
		if node == nil {
			continue
		}
		distance := EditDistance(p, path, allowReplacements, maxValidEditDistance)
		if distance < minDistance {
			minDistance = distance
			result = node
		}
	}
	return result
}

func (s *State) AddIn(edge *Edge, path string, slashBits uint64) {
	node := s.GetNode(path, slashBits)
	node.generated_by_dep_loader_ = false
	edge.inputs_ = append(edge.inputs_, node)
	node.AddOutEdge(edge)
}

func (s *State) AddOut(edge *Edge, path string, slashBits uint64, err *string) bool {
	node := s.GetNode(path, slashBits)
	if other := node.in_edge(); other != nil {
		if other == edge {
			*err = fmt.Sprintf("%s is defined as an output multiple times", path)
		} else {
			*err = fmt.Sprintf("multiple rules generate %s", path)
		}
		return false
	}
	edge.outputs_ = append(edge.outputs_, node)
	node.set_in_edge(edge)
	node.generated_by_dep_loader_ = false
	return true
}

func (s *State) AddValidation(edge *Edge, path string, slashBits uint64) {
	node := s.GetNode(path, slashBits)
	edge.validations_ = append(edge.validations_, node)
	node.AddValidationOutEdge(edge)
	node.generated_by_dep_loader_ = false
}

func (s *State) AddDefault(path string, err *string) bool {
	node := s.LookupNode(path)
	if node == nil {
		*err = "unknown target '" + path + "'"
		return false
	}
	s.defaults_ = append(s.defaults_, node)
	return true
}

func (s *State) Reset() {
	for _, n := range s.paths_ {
		n.ResetState()
	}
	for _, e := range s.edges_ {
		e.outputs_ready_ = false
		e.deps_loaded_ = false
		e.mark_ = VisitNone
	}
}

func (s *State) RootNodes(err *string) []*Node {
	var root_nodes []*Node
	for _, e := range s.edges_ {
		for _, out := range e.outputs_ {
			if len(out.out_edges_) == 0 {
				root_nodes = append(root_nodes, out)
			}
		}
	}
	if len(s.edges_) > 0 && len(root_nodes) == 0 {
		*err = "could not determine root nodes_ of build graph"
	}
	return root_nodes
}

func (s *State) Dump() {
	for _, node := range s.paths_ {
		status := "unknown"
		if node.StatusKnown() {
			if node.dirty_ {
				status = "dirty"
			} else {
				status = "clean"
			}
		}
		fmt.Printf("%s %s [id:%d]\n", node.path_, status, node.id_)
	}
	if len(s.pools_) > 0 {
		fmt.Printf("resource_pools:\n")
		for _, pool := range s.pools_ {
			if pool.Name != "" {
				pool.Dump()
			}
		}
	}
}

func (s *State) DefaultNodes(err *string) []*Node {
	if len(s.defaults_) > 0 {
		return s.defaults_
	}
	roots := s.RootNodes(err)
	return roots
}
