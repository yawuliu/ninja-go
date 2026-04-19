package builder

import (
	"fmt"
	"ninja-go/pkg/util"
	"sync"
)

func paths(nodes []*Node) []string {
	res := make([]string, len(nodes))
	for i, n := range nodes {
		res[i] = n.Path
	}
	return res
}

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

var (
	DefaultPool = NewPool("", 0)
	ConsolePool = NewPool("console", 1)
)

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
	s.AddPool(DefaultPool)
	s.AddPool(ConsolePool)
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
		Pool: DefaultPool,
		Env:  s.Bindings,
		ID:   uint64(len(s.Edges)),
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
	n.ID = s.nextID
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
	norm := util.NormalizePath(path)
	if n, ok := s.Paths[norm]; ok {
		return n
	}
	n := &Node{
		Path:                 norm,
		SlashBits:            slashBits,
		ID:                   s.nextID,
		GeneratedByDepLoader: true,
	}
	s.nextID++
	s.Paths[norm] = n
	s.nodesByID = append(s.nodesByID, n)
	return n
}

func (s *State) LookupNode(path string) *Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	norm := util.NormalizePath(path)
	return s.Paths[norm]
}

func (s *State) SpellcheckNode(path string) *Node {
	// 简单实现：返回编辑距离最小的节点（暂略）
	return nil
}

func (s *State) AddIn(edge *Edge, path string, slashBits uint64) {
	node := s.GetNode(path, slashBits)
	node.GeneratedByDepLoader = false
	edge.Inputs = append(edge.Inputs, node)
	node.AddOutEdge(edge)
}

func (s *State) AddOut(edge *Edge, path string, slashBits uint64) error {
	node := s.GetNode(path, slashBits)
	if other := node.InEdge; other != nil {
		if other == edge {
			return fmt.Errorf("%s is defined as an output multiple times", path)
		}
		return fmt.Errorf("multiple rules generate %s", path)
	}
	edge.Outputs = append(edge.Outputs, node)
	node.InEdge = edge
	node.GeneratedByDepLoader = false
	return nil
}

func (s *State) AddValidation(edge *Edge, path string, slashBits uint64) {
	node := s.GetNode(path, slashBits)
	edge.Validations = append(edge.Validations, node)
	node.AddValidationOutEdge(edge)
	node.GeneratedByDepLoader = false
}

func (s *State) AddDefault(path string) error {
	node := s.LookupNode(path)
	if node == nil {
		return fmt.Errorf("unknown target '%s'", path)
	}
	s.Defaults = append(s.Defaults, node)
	return nil
}

func (s *State) Reset() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, n := range s.Paths {
		n.ResetState()
	}
	for _, e := range s.Edges {
		e.OutputsReady = false
		e.DepsLoaded = false
		e.Mark = VisitNone
	}
}

func (s *State) RootNodes() ([]*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var roots []*Node
	for _, e := range s.Edges {
		for _, out := range e.Outputs {
			if len(out.OutEdges) == 0 {
				roots = append(roots, out)
			}
		}
	}
	if len(s.Edges) > 0 && len(roots) == 0 {
		return nil, fmt.Errorf("could not determine root nodes of build graph")
	}
	return roots, nil
}

func (s *State) DefaultNodes() []*Node {
	if len(s.Defaults) > 0 {
		return s.Defaults
	}
	roots, _ := s.RootNodes()
	return roots
}
