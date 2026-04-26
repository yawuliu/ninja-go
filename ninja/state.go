package main

import (
	"fmt"
	"sync"
)

var kDefaultPool *Pool = NewPool("", 0)
var kConsolePool *Pool = NewPool("console", 1)

type State struct {
	mu        sync.RWMutex
	Paths     map[string]*Node
	Pools     map[string]*Pool
	Rules     map[string]*Rule
	Edges     []*Edge
	Bindings  *BindingEnv
	Defaults  []*Node // default 语句指定的目标
	nextID    int
	nodesByID []*Node
}

func NewState() *State {
	s := &State{
		Paths:     make(map[string]*Node),
		Pools:     make(map[string]*Pool),
		Edges:     []*Edge{},
		Rules:     make(map[string]*Rule),
		Bindings:  NewBindingEnv(nil),
		Defaults:  []*Node{},
		nodesByID: []*Node{},
	}
	s.Bindings.AddRule(PhonyRule())
	s.AddPool(kDefaultPool)
	s.AddPool(kConsolePool)
	return s
}

func (s *State) AddPool(pool *Pool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Pools[pool.Name] = pool
}

func (s *State) LookupPool(name string) *Pool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Pools[name]
}

func (s *State) AddEdge(rule *Rule) *Edge {
	s.mu.Lock()
	defer s.mu.Unlock()
	edge := &Edge{
		Rule: rule,
		Pool: kDefaultPool,
		env_: s.Bindings,
		id_:  uint64(len(s.Edges)),
	}
	s.Edges = append(s.Edges, edge)
	return edge
}

func (s *State) AddNode(path string, slashBits uint64) *Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.Paths[path]; ok {
		return n
	}
	n := NewNode(path, slashBits)
	n.id_ = s.nextID
	s.nextID++
	s.Paths[path] = n
	s.nodesByID = append(s.nodesByID, n)
	return n
}

func (s *State) GetNodeByID(id int) *Node {
	if id >= 0 && id < len(s.nodesByID) {
		return s.nodesByID[id]
	}
	return nil
}

// GetNode 返回指定路径的节点，如果不存在则创建（与 AddNode 相同，但语义更清晰）
func (s *State) GetNode(path string, slashBits uint64) *Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	norm := NormalizePath(path)
	if n, ok := s.Paths[norm]; ok {
		return n
	}
	n := &Node{
		path_:                    norm,
		slash_bits_:              slashBits,
		id_:                      s.nextID,
		generated_by_dep_loader_: true,
	}
	s.nextID++
	s.Paths[norm] = n
	s.nodesByID = append(s.nodesByID, n)
	return n
}

func (s *State) LookupNode(path string) *Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	norm := NormalizePath(path)
	return s.Paths[norm]
}

func (s *State) SpellcheckNode(path string) *Node {
	const allowReplacements = true
	const maxValidEditDistance = 3

	minDistance := maxValidEditDistance + 1
	var result *Node

	for p, node := range s.Paths {
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
	s.Defaults = append(s.Defaults, node)
	return true
}

func (s *State) Reset() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, n := range s.Paths {
		n.ResetState()
	}
	for _, e := range s.Edges {
		e.outputs_ready_ = false
		e.deps_loaded_ = false
		e.mark_ = VisitNone
	}
}

func (s *State) RootNodes(err *string) []*Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var root_nodes []*Node
	for _, e := range s.Edges {
		for _, out := range e.outputs_ {
			if len(out.out_edges_) == 0 {
				root_nodes = append(root_nodes, out)
			}
		}
	}
	if len(s.Edges) > 0 && len(root_nodes) == 0 {
		*err = "could not determine root nodes of build graph"
	}
	return root_nodes
}

func (s *State) DefaultNodes(err *string) []*Node {
	if len(s.Defaults) > 0 {
		return s.Defaults
	}
	roots := s.RootNodes(err)
	return roots
}
