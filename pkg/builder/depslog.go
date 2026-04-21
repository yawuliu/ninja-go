package builder

import (
	"bytes"
	"encoding/binary"
	"ninja-go/pkg/util"
	"os"
	"sync"
	"syscall"
)

const (
	// kFileSignature is the file signature written at the beginning of the deps log.
	kFileSignature = "# ninjadeps\n"
	// kFileSignatureSize is the length of the signature (excluding null terminator).
	kFileSignatureSize = len(kFileSignature)
)

// kCurrentVersion is the current version of the deps log format.
const kCurrentVersion int = 4

// kMaxRecordSize is the maximum allowed size of a deps log record (in bytes).
const kMaxRecordSize = (1 << 19) - 1 // 524287
// Deps 表示一个输出的依赖列表
type Deps struct {
	mtime      int64
	nodes      []*Node
	node_count int
}

func NewDeps(mtime int64, node_count int) *Deps {
	d := &Deps{mtime: mtime, node_count: node_count}
	d.nodes = make([]*Node, node_count)
	return d
}

type DepsLog struct {
	mu                sync.RWMutex
	filePath          string
	file              *os.File
	deps              []*Deps       // 索引为节点 ID
	nodes             []*Node       // 节点 ID -> Node
	reverseDeps       map[int][]int // 依赖节点 ID -> 被依赖的输出节点 ID 列表
	needsRecompaction bool
}

func NewDepsLog(path string) *DepsLog {
	return &DepsLog{
		filePath:    path,
		deps:        []*Deps{},
		nodes:       []*Node{},
		reverseDeps: make(map[int][]int),
	}
}
func (dl *DepsLog) Close() error {
	dl.openForWriteIfNeeded()
	if dl.file != nil {
		err := dl.file.Close()
		dl.file = nil
		return err
	}
	return nil
}

func (dl *DepsLog) OpenForWrite(path string, err *string) bool {
	if dl.needsRecompaction {
		if !dl.Recompact(path, err) {
			return false
		}
	}
	dl.filePath = path
	return true
}

func (dl *DepsLog) openForWriteIfNeeded() bool {
	if dl.file != nil || dl.filePath == "" {
		return true
	}
	f, err := os.OpenFile(dl.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false
	}
	dl.file = f
	// 检查文件是否为空
	info, err := f.Stat()
	if err != nil {
		return false
	}
	if info.Size() == 0 {
		if _, err := f.Write([]byte(kFileSignature)); err != nil {
			return false
		}
		if err := binary.Write(f, binary.LittleEndian, int32(kCurrentVersion)); err != nil {
			return false
		}
	}
	return true
}
func (d *DepsLog) RecordDeps1(node *Node, mtime int64, nodes []*Node) bool {
	return d.RecordDeps(node, mtime, len(nodes), nodes)
}

// RecordDeps 记录输出节点的依赖
func (d *DepsLog) RecordDeps(node *Node, mtime int64, nodeCount int, nodes []*Node) bool {
	// Track whether there's any new data to be recorded.
	madeChange := false

	// Assign ids to all nodes that are missing one.
	if node.id() < 0 {
		if !d.RecordId(node) {
			return false
		}
		madeChange = true
	}
	for i := 0; i < nodeCount; i++ {
		if nodes[i].id() < 0 {
			if !d.RecordId(nodes[i]) {
				return false
			}
			madeChange = true
		}
	}

	// See if the new data is different than the existing data, if any.
	if !madeChange {
		deps := d.GetDeps(node)
		if deps == nil ||
			deps.mtime != mtime ||
			deps.node_count != nodeCount {
			madeChange = true
		} else {
			for i := 0; i < nodeCount; i++ {
				if deps.nodes[i] != nodes[i] {
					madeChange = true
					break
				}
			}
		}
	}

	// Don't write anything if there's no new info.
	if !madeChange {
		return true
	}

	// Update on-disk representation.
	size := 4 * (1 + 2 + nodeCount)
	if size > kMaxRecordSize {
		// errno = ERANGE (not directly translatable; return false)
		return false
	}

	if !d.OpenForWriteIfNeeded() {
		return false
	}
	size |= 0x80000000 // Deps record: set high bit.

	// Write size
	if err := binary.Write(d.file, binary.LittleEndian, uint32(size)); err != nil {
		return false
	}
	// Write node id
	id := int32(node.id())
	if err := binary.Write(d.file, binary.LittleEndian, id); err != nil {
		return false
	}
	// Write mtime low 32 bits
	mtimeLow := uint32(mtime & 0xffffffff)
	if err := binary.Write(d.file, binary.LittleEndian, mtimeLow); err != nil {
		return false
	}
	// Write mtime high 32 bits
	mtimeHigh := uint32((mtime >> 32) & 0xffffffff)
	if err := binary.Write(d.file, binary.LittleEndian, mtimeHigh); err != nil {
		return false
	}
	// Write each dependency node id
	for i := 0; i < nodeCount; i++ {
		id = int32(nodes[i].id())
		if err := binary.Write(d.file, binary.LittleEndian, id); err != nil {
			return false
		}
	}
	// Flush
	if err := d.file.Sync(); err != nil {
		return false
	}

	// Update in-memory representation.
	deps := &Deps{mtime: mtime, node_count: nodeCount, nodes: make([]*Node, nodeCount)}
	for i := 0; i < nodeCount; i++ {
		deps.nodes[i] = nodes[i]
	}
	d.UpdateDeps(node.id(), deps)

	return true
}

func (dl *DepsLog) RecordId(node *Node) bool {
	path := node.Path
	pathSize := len(path)
	padding := (4 - (pathSize+1)%4) % 4
	size := 4 + pathSize + 1 + padding // 4字节校验和 + 路径 + null + 填充
	if size > kMaxRecordSize {
		panic("node path too long")
		return false
	}
	if !dl.openForWriteIfNeeded() {
		return false
	}
	// 写入节点记录（最高位为0）
	recordSize := uint32(size)
	if err := binary.Write(dl.file, binary.LittleEndian, recordSize); err != nil {
		return false
	}
	if _, err := dl.file.Write([]byte(path)); err != nil {
		return false
	}
	if _, err := dl.file.Write([]byte{0}); err != nil {
		return false
	}
	if padding > 0 {
		if _, err := dl.file.Write(make([]byte, padding)); err != nil {
			return false
		}
	}
	id := len(dl.nodes)
	checksum := uint32(^id)
	if err := binary.Write(dl.file, binary.LittleEndian, checksum); err != nil {
		return false
	}
	node.id_ = id
	dl.nodes = append(dl.nodes, node)
	return true
}

// Load 从磁盘加载 .ninja_deps 文件
func (dl *DepsLog) Load(state *State) error {
	f, err := os.Open(dl.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	// 读取签名
	var sig [kFileSignatureSize]byte
	if _, err := f.Read(sig[:]); err != nil {
		return err
	}
	if string(sig[:]) != kFileSignature {
		// 无效签名，删除文件并视为空
		f.Close()
		os.Remove(dl.filePath)
		return nil
	}
	var version int
	if err := binary.Read(f, binary.LittleEndian, &version); err != nil {
		return err
	}
	if version != kCurrentVersion {
		// 版本不匹配，删除文件并视为空
		f.Close()
		os.Remove(dl.filePath)
		return nil
	}

	dl.mu.Lock()
	defer dl.mu.Unlock()
	dl.deps = []*Deps{}
	dl.nodes = []*Node{}
	dl.reverseDeps = make(map[int][]int)
	offset := int64(len(kFileSignature) + 4) // 已读字节数
	uniqueCount := 0
	totalCount := 0

	for {
		var size uint32
		if err := binary.Read(f, binary.LittleEndian, &size); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return err
		}
		isDeps := (size >> 31) != 0
		size = size & 0x7FFFFFFF
		if size > kMaxRecordSize {
			// 文件损坏，尝试截断恢复
			break
		}
		record := make([]byte, size)
		if _, err := f.Read(record); err != nil {
			// 读取失败，截断到当前偏移
			break
		}
		r := bytes.NewReader(record)
		offset += int64(size) + 4

		if isDeps {
			// 依赖记录
			var outID int32
			if err := binary.Read(r, binary.LittleEndian, &outID); err != nil {
				break
			}
			var mtimeLow, mtimeHigh uint32
			if err := binary.Read(r, binary.LittleEndian, &mtimeLow); err != nil {
				break
			}
			if err := binary.Read(r, binary.LittleEndian, &mtimeHigh); err != nil {
				break
			}
			mtime := int64(mtimeHigh)<<32 | int64(mtimeLow)
			depCount := (size - 12) / 4
			if depCount > 0 {
				depIDs := make([]int32, depCount)
				for i := 0; i < int(depCount); i++ {
					if err := binary.Read(r, binary.LittleEndian, &depIDs[i]); err != nil {
						break
					}
				}
				// 检查所有依赖节点 ID 是否有效
				valid := true
				for _, id := range depIDs {
					if int(id) >= len(dl.nodes) || dl.nodes[id] == nil {
						valid = false
						break
					}
				}
				if !valid {
					break
				}
				// 获取输出节点
				if int(outID) >= len(dl.nodes) || dl.nodes[outID] == nil {
					break
				}
				outNode := dl.nodes[outID]
				var deps []*Node
				for _, id := range depIDs {
					deps = append(deps, dl.nodes[id])
				}
				idx := outNode.id_
				dl.ensureCapacity(idx)
				dl.deps[idx] = &Deps{mtime: mtime, nodes: deps}
				dl.nodes[idx] = outNode
				for _, dep := range deps {
					dl.reverseDeps[dep.id_] = append(dl.reverseDeps[dep.id_], idx)
				}
				totalCount++
				uniqueCount++
			}
		} else {
			// 节点记录
			// 读取路径直到 null
			var pathBytes []byte
			for {
				b, err := r.ReadByte()
				if err != nil {
					break
				}
				if b == 0 {
					break
				}
				pathBytes = append(pathBytes, b)
			}
			if len(pathBytes) == 0 {
				break
			}
			path := string(pathBytes)
			// 跳过填充
			padding := (4 - (len(path)+1)%4) % 4
			if _, err := r.Seek(int64(padding), os.SEEK_CUR); err != nil {
				break
			}
			var checksum uint32
			if err := binary.Read(r, binary.LittleEndian, &checksum); err != nil {
				break
			}
			expectedID := int(^checksum)
			// 创建节点
			node := state.GetNode(path, 0) // slash_bits 不重要
			if node.id_ >= 0 {
				// 节点已有 ID，冲突
				break
			}
			node.id_ = expectedID
			dl.ensureCapacity(expectedID)
			dl.nodes[expectedID] = node
		}
	}

	// 如果读取失败，截断文件到最后一个有效记录位置
	if offset > 0 {
		if err := os.Truncate(dl.filePath, offset); err != nil {
			return err
		}
	}

	// 判断是否需要 recompact
	const kMinCompactionEntryCount = 1000
	const kCompactionRatio = 3
	if totalCount > kMinCompactionEntryCount && totalCount > uniqueCount*kCompactionRatio {
		dl.needsRecompaction = true
	}
	return nil
}

// GetDeps 获取输出节点的依赖
func (dl *DepsLog) GetDeps(out *Node) *Deps {
	if out.id_ < 0 || out.id_ >= len(dl.deps) {
		return nil
	}
	return dl.deps[out.id_]
}

// GetFirstReverseDepsNode 获取依赖某个节点的第一个输出节点
func (dl *DepsLog) GetFirstReverseDepsNode(dep *Node) *Node {
	revs, ok := dl.reverseDeps[dep.id_]
	if !ok || len(revs) == 0 {
		return nil
	}
	return dl.nodes[revs[0]]
}

// Recompact 重新压实日志
func (dl *DepsLog) Recompact(path string, err *string) bool {
	dl.Close()
	temp_path := path + ".recompact"
	os.Remove(temp_path)

	newLog := NewDepsLog(temp_path)
	if !newLog.OpenForWrite(temp_path, err) {
		return false
	}
	// 重置所有节点的 ID
	for _, n := range dl.nodes {
		if n != nil {
			n.id_ = -1
		}
	}
	// 重新记录所有存活的依赖
	for id, deps := range dl.deps {
		if deps == nil {
			continue
		}
		outNode := dl.nodes[id]
		if outNode == nil || !dl.isDepsEntryLive(outNode) {
			continue
		}
		if !newLog.RecordDeps(outNode, deps.mtime, len(deps.nodes), deps.nodes) {
			newLog.Close()
			return false
		}
	}
	newLog.Close()

	// 替换内存数据
	dl.deps = newLog.deps
	dl.nodes = newLog.nodes

	return util.ReplaceContent(path, temp_path, err)
}

// isDepsEntryLive 判断节点的依赖记录是否存活
func (dl *DepsLog) isDepsEntryLive(node *Node) bool {
	if node.InEdge == nil {
		return false
	}
	// 检查边的规则是否有 "deps" 属性
	return node.InEdge.GetBinding("deps") != ""
}

func (dl *DepsLog) UpdateDeps(outID int, deps *Deps) bool {
	dl.ensureCapacity(outID)
	old := dl.deps[outID]
	dl.deps[outID] = deps
	return old != nil
}

func (dl *DepsLog) ensureCapacity(id int) {
	if id >= len(dl.deps) {
		dl.deps = append(dl.deps, make([]*Deps, id+1-len(dl.deps))...)
		dl.nodes = append(dl.nodes, make([]*Node, id+1-len(dl.nodes))...)
	}
}

// / Used for tests.
func (dl *DepsLog) Nodes() []*Node { return dl.nodes }

func (dl *DepsLog) Deps() []*Deps { return dl.deps }

// IsDepsEntryLiveFor 判断节点的依赖记录是否应该保留。
// 节点必须有入边，且该边的 "deps" 绑定非空。
func IsDepsEntryLiveFor(node *Node) bool {
	if node.InEdge == nil {
		return false
	}
	return node.InEdge.GetBinding("deps") != ""
}
func (d *DepsLog) OpenForWriteIfNeeded() bool {
	if d.filePath == "" {
		return true
	}

	var err error
	d.file, err = os.OpenFile(d.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return false
	}

	// Set buffer size to kMaxRecordSize+1 (simulate setvbuf)
	// In Go we can't set buffer on os.File directly; we rely on Sync after writes.
	// For consistency, we'll just use the file as is.

	// Set close-on-exec
	if fd := d.file.Fd(); fd > 0 {
		syscall.CloseOnExec(syscall.Handle(fd))
	}

	// In append mode, file pointer is already at end; but ensure we're at end.
	if _, err := d.file.Seek(0, os.SEEK_END); err != nil {
		return false
	}

	// Check if file is empty
	stat, err := d.file.Stat()
	if err != nil {
		return false
	}
	if stat.Size() == 0 {
		// Write signature
		if _, err := d.file.Write([]byte(kFileSignature)); err != nil {
			return false
		}
		// Write version
		if err := binary.Write(d.file, binary.LittleEndian, kCurrentVersion); err != nil {
			return false
		}
	}

	// Flush
	if err := d.file.Sync(); err != nil {
		return false
	}

	d.filePath = ""
	return true
}
