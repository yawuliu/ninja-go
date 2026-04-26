package main

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
	path_                    string
	slash_bits_              uint64
	mtime_                   int64
	exists_                  ExistenceStatus // -1 unknown, 0 missing, 1 exists
	dirty_                   bool
	dyndep_pending_          bool
	generated_by_dep_loader_ bool
	id_                      int
	in_edge_                 *Edge
	out_edges_               []*Edge
	validation_out_edges_    []*Edge
}

func NewNode(path string, slashBits uint64) *Node {
	return &Node{
		path_:                    path,
		slash_bits_:              slashBits,
		mtime_:                   -1,
		exists_:                  ExistenceStatusUnknown,
		generated_by_dep_loader_: true,
		id_:                      -1,
	}
}
func (n *Node) id() int       { return n.id_ }
func (n *Node) set_id(id int) { n.id_ = id }

func (n *Node) Stat(diskInterface FileSystem, err *string) bool {
	n.mtime_ = diskInterface.Stat(n.path_, err)
	if n.mtime_ == -1 {
		return false
	}
	if n.mtime_ != 0 {
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
func (n *Node) StatIfNecessary(fs FileSystem, err *string) bool {
	if n.StatusKnown() {
		return true
	}
	return n.Stat(fs, err)
}

func (n *Node) ResetState() {
	n.mtime_ = -1
	n.exists_ = ExistenceStatusUnknown
	n.dirty_ = false
}

func (n *Node) MarkMissing() {
	if n.mtime_ == -1 {
		n.mtime_ = 0
	}
	n.exists_ = ExistenceStatusMissing
}

func (n *Node) AddOutEdge(edge *Edge) {
	// 避免重复添加
	for _, e := range n.out_edges_ {
		if e == edge {
			return
		}
	}
	n.out_edges_ = append(n.out_edges_, edge)
}

func (n *Node) IsExists() bool {
	return n.exists_ == ExistenceStatusExists
}

func (n *Node) AddValidationOutEdge(e *Edge) {
	n.validation_out_edges_ = append(n.validation_out_edges_, e)
}

func (n *Node) Dirty() bool {
	return n.dirty_
}

// UpdatePhonyMtime 更新 phony 节点的 mtime。
// 如果节点不存在（即磁盘上没有该文件），则将其 mtime 设置为当前 mtime 和已有 mtime 中的较大值。
func (n *Node) UpdatePhonyMtime(mtime int64) {
	if !n.IsExists() {
		if mtime > n.mtime_ {
			n.mtime_ = mtime
		}
	}
}

func (n *Node) in_edge() *Edge         { return n.in_edge_ }
func (n *Node) set_in_edge(edge *Edge) { n.in_edge_ = edge }

func (n *Node) SetDirty(dirty bool) { n.dirty_ = dirty }
func (n *Node) MarkDirty()          { n.dirty_ = true }
