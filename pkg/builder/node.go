package builder

import (
	"ninja-go/pkg/util"
)

type Node struct {
	Path                 string
	SlashBits            uint64
	Mtime                int64
	exists_              int8 // -1 unknown, 0 missing, 1 exists
	dirty_               bool
	DyndepPending        bool
	GeneratedByDepLoader bool
	id_                  int
	InEdge               *Edge
	OutEdges             []*Edge
	ValidationOutEdges   []*Edge
}

const (
	ExistenceUnknown = -1
	ExistenceMissing = 0
	ExistenceExists  = 1
)

func NewNode(path string, slashBits uint64) *Node {
	return &Node{
		Path:                 path,
		SlashBits:            slashBits,
		Mtime:                -1,
		exists_:              ExistenceUnknown,
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
		n.exists_ = ExistenceExists
	} else {
		n.exists_ = ExistenceMissing
	}
	return true
}
func (n *Node) Exists() bool {
	return n.exists_ == ExistenceExists
}

func (n *Node) StatusKnown() bool {
	return n.exists_ != ExistenceUnknown
}
func (n *Node) StatIfNecessary(fs util.FileSystem, err *string) bool {
	if n.StatusKnown() {
		return true
	}
	return n.Stat(fs, err)
}

func (n *Node) ResetState() {
	n.Mtime = -1
	n.exists_ = ExistenceUnknown
	n.dirty_ = false
}

func (n *Node) MarkMissing() {
	if n.Mtime == -1 {
		n.Mtime = 0
	}
	n.exists_ = ExistenceMissing
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
	return n.exists_ == ExistenceExists
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
