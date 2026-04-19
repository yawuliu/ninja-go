package builder

import (
	"ninja-go/pkg/util"
	"os"
)

type Node struct {
	Path                 string
	SlashBits            uint64
	Mtime                int64
	Exists               int8 // -1 unknown, 0 missing, 1 exists
	Dirty                bool
	DyndepPending        bool
	GeneratedByDepLoader bool
	ID                   int
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
		Exists:               ExistenceUnknown,
		GeneratedByDepLoader: true,
		ID:                   -1,
	}
}

func (n *Node) Stat(fs util.FileSystem) error {
	info, err := fs.Stat(n.Path)
	if err != nil {
		if os.IsNotExist(err) {
			n.Mtime = 0
			n.Exists = ExistenceMissing
			return nil
		}
		return err
	}
	n.Mtime = info.ModTime().UnixNano()
	n.Exists = ExistenceExists
	return nil
}

func (n *Node) StatIfNecessary(fs util.FileSystem) error {
	if n.Exists != ExistenceUnknown {
		return nil
	}
	return n.Stat(fs)
}

func (n *Node) ResetState() {
	n.Mtime = -1
	n.Exists = ExistenceUnknown
	n.Dirty = false
}

func (n *Node) MarkMissing() {
	if n.Mtime == -1 {
		n.Mtime = 0
	}
	n.Exists = ExistenceMissing
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
	return n.Exists == ExistenceExists
}

func (n *Node) AddValidationOutEdge(e *Edge) {
	n.ValidationOutEdges = append(n.ValidationOutEdges, e)
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
