package graph

import (
	"ninja-go/pkg/util"
	"os"
	"sync"
)

type Node struct {
	Path      string
	Mtime     int64 // 文件修改时间（纳秒），-1 表示不存在
	Dirty     bool
	Generated bool
	Edge      *Edge   // 产生该节点的边（nil 表示源文件）
	OutEdges  []*Edge // 依赖该节点的边（即以此节点为输入的边）
}

type Rule struct {
	Name      string
	Command   string // 可包含 $var
	Depfile   string
	Dyndep    string
	Restat    bool
	Generator bool
	Pool      string
}

type Pool struct {
	Name  string
	Depth int
}

type Edge struct {
	Rule          *Rule
	Inputs        []*Node
	Outputs       []*Node
	ImplicitDeps  []*Node // | 后的隐式依赖, 隐式依赖（如头文件）
	OrderOnlyDeps []*Node // || 后的 order-only 依赖
	DyndepFile    *Node
	Pool          *Pool
}

type State struct {
	mu       sync.RWMutex
	Pools    map[string]*Pool
	Rules    map[string]*Rule
	Edges    []*Edge
	Nodes    map[string]*Node
	Defaults []*Node // default 语句指定的目标
}

func NewState() *State {
	return &State{
		Pools:    make(map[string]*Pool),
		Rules:    make(map[string]*Rule),
		Edges:    []*Edge{},
		Nodes:    make(map[string]*Node),
		Defaults: []*Node{},
	}
}

func (s *State) AddNode(path string) *Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.Nodes[path]; ok {
		return n
	}
	n := &Node{Path: path, Mtime: -1}
	s.Nodes[path] = n
	return n
}

func (n *Node) AddOutEdge(edge *Edge) {
	// 避免重复添加
	for _, e := range n.OutEdges {
		if e == edge {
			return
		}
	}
	n.OutEdges = append(n.OutEdges, edge)
}

func (n *Node) LoadMtime(fs util.FileSystem) error {
	nativePath := util.ToNativePath(n.Path)
	info, err := fs.Stat(nativePath)
	if err != nil {
		if os.IsNotExist(err) {
			n.Mtime = -1
			return nil
		}
		return err
	}
	n.Mtime = info.ModTime().UnixNano()
	return nil
}

func (n *Node) IsDirty() bool {
	return n.Dirty
}

func (s *State) LookupNode(path string) *Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Nodes[path]
}
