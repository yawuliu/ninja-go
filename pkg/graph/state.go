package graph

import (
	"ninja-go/pkg/util"
	"os"
	"strings"
	"sync"
)

type Node struct {
	ID        int // 节点唯一标识，由 State 分配
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
	// Deps          string // 新增：deps = gcc 或 msvc
	ImplicitOuts int // 隐式输出数量
}

func (e *Edge) EvaluateCommand() string {
	// 展开命令中的 $in, $out 等变量
	// 可以复用 builder.expandCommand 的逻辑，但这里需要独立实现
	cmd := e.Rule.Command
	// 简单替换（实际应更复杂，支持转义等）
	inPaths := paths(e.Inputs)
	outPaths := paths(e.Outputs)
	cmd = strings.ReplaceAll(cmd, "$in", strings.Join(inPaths, " "))
	cmd = strings.ReplaceAll(cmd, "$out", strings.Join(outPaths, " "))
	if e.Rule.Depfile != "" {
		depfile := strings.ReplaceAll(e.Rule.Depfile, "$out", e.Outputs[0].Path)
		cmd = strings.ReplaceAll(cmd, "$depfile", depfile)
	}
	cmd = strings.ReplaceAll(cmd, "$$", "$")
	return cmd
}

func (e *Edge) GetBinding(key string) string {
	// 先查 Edge 自身的 binding（如果有），否则查 Rule 的 binding
	// 当前 Edge 没有独立的 binding map，需要扩展
	// 简化：只返回 Rule 上的属性
	switch key {
	case "depfile":
		return e.Rule.Depfile
	case "dyndep":
		return e.Rule.Dyndep
	case "restat":
		if e.Rule.Restat {
			return "1"
		}
		return ""
	default:
		return ""
	}
}

func paths(nodes []*Node) []string {
	res := make([]string, len(nodes))
	for i, n := range nodes {
		res[i] = n.Path
	}
	return res
}

type State struct {
	mu       sync.RWMutex
	Pools    map[string]*Pool
	Rules    map[string]*Rule
	Edges    []*Edge
	Nodes    map[string]*Node
	Defaults []*Node // default 语句指定的目标
	// nextID    int
	//nodesByID []*Node
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
	n := &Node{Path: path, Mtime: -1, ID: -1}
	// s.nextID++
	s.Nodes[path] = n
	// s.nodesByID = append(s.nodesByID, n)
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

//func (s *State) GetNodeByID(id int) *Node {
//	s.mu.RLock()
//	defer s.mu.RUnlock()
//	if id >= 0 && id < len(s.nodesByID) {
//		return s.nodesByID[id]
//	}
//	return nil
//}

// GetNode 返回指定路径的节点，如果不存在则创建（与 AddNode 相同，但语义更清晰）
func (s *State) GetNode(path string) *Node {
	return s.AddNode(path)
}

func (s *State) RootNodes() []*Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var roots []*Node
	for _, n := range s.Nodes {
		// 如果节点没有入边（即没有其他节点依赖它？实际应为没有出边？）
		// 根据 C++ 实现，RootNodes 返回那些不被任何边作为输出的节点（即源文件）
		// 但 Ninja 中更常见的是返回没有被其他节点依赖的节点（即最终目标）
		// 我们简化：返回所有没有 out_edges 的节点（即没有其他边以此节点为输入）
		if len(n.OutEdges) == 0 {
			roots = append(roots, n)
		}
	}
	return roots
}
