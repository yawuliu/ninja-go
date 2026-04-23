package main

import (
	"ninja-go/ninja/util"
)

// ExistenceStatus 表示文件存在性状态
type ExistenceStatus int8

const (
	// ExistenceStatusUnknown 文件尚未被检查
	ExistenceStatusUnknown ExistenceStatus = iota
	// ExistenceStatusMissing 文件不存在，mtime_ 将是最新依赖的 mtime
	ExistenceStatusMissing
	// ExistenceStatusExists 路径是一个实际存在的文件，mtime_ 将是文件的 mtime
	ExistenceStatusExists
)

// String 实现 fmt.Stringer 接口
func (s ExistenceStatus) String() string {
	switch s {
	case ExistenceStatusUnknown:
		return "Unknown"
	case ExistenceStatusMissing:
		return "Missing"
	case ExistenceStatusExists:
		return "Exists"
	default:
		return "Invalid"
	}
}

type Node struct {
	Path                 string
	SlashBits            uint64
	Mtime                int64
	exists_              ExistenceStatus // -1 unknown, 0 missing, 1 exists
	dirty_               bool
	DyndepPending        bool
	GeneratedByDepLoader bool
	id_                  int
	InEdge               *Edge
	OutEdges             []*Edge
	ValidationOutEdges   []*Edge
}

func NewNode(path string, slashBits uint64) *Node {
	return &Node{
		Path:                 path,
		SlashBits:            slashBits,
		Mtime:                -1,
		exists_:              ExistenceStatusUnknown,
		GeneratedByDepLoader: true,
		id_:                  -1,
	}
}
func (n *Node) id() int       { return n.id_ }
func (n *Node) set_id(id int) { n.id_ = id }

func (n *Node) Stat(diskInterface util.FileSystem, err *string) bool {
	n.Mtime = diskInterface.Stat(n.Path, err)
	if n.Mtime == -1 {
		return false
	}
	if n.Mtime != 0 {
		n.exists_ = ExistenceStatusExists
	} else {
		n.exists_ = ExistenceStatusMissing
	}
	return true
}
func (n *Node) Exists() bool {
	return n.exists_ == ExistenceStatusExists
}

func (n *Node) StatusKnown() bool {
	return n.exists_ != ExistenceStatusUnknown
}
func (n *Node) StatIfNecessary(fs util.FileSystem, err *string) bool {
	if n.StatusKnown() {
		return true
	}
	return n.Stat(fs, err)
}

func (n *Node) ResetState() {
	n.Mtime = -1
	n.exists_ = ExistenceStatusUnknown
	n.dirty_ = false
}

func (n *Node) MarkMissing() {
	if n.Mtime == -1 {
		n.Mtime = 0
	}
	n.exists_ = ExistenceStatusMissing
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

func (n *Node) IsExists() bool {
	return n.exists_ == ExistenceStatusExists
}

func (n *Node) AddValidationOutEdge(e *Edge) {
	n.ValidationOutEdges = append(n.ValidationOutEdges, e)
}

func (n *Node) Dirty() bool {
	return n.dirty_
}

// UpdatePhonyMtime 更新 phony 节点的 mtime。
// 如果节点不存在（即磁盘上没有该文件），则将其 mtime 设置为当前 mtime 和已有 mtime 中的较大值。
func (n *Node) UpdatePhonyMtime(mtime int64) {
	if !n.IsExists() {
		if mtime > n.Mtime {
			n.Mtime = mtime
		}
	}
}

func (n *Node) in_edge() *Edge         { return n.InEdge }
func (n *Node) set_in_edge(edge *Edge) { n.InEdge = edge }

func (n *Node) SetDirty(dirty bool) { n.dirty_ = dirty }
func (n *Node) MarkDirty()          { n.dirty_ = true }
