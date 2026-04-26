package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"syscall"
)

const (
	// kFileSignature is the log_file_ signature written at the beginning of the deps_ log.
	kFileSignature = "# ninjadeps\n"
	// kFileSignatureSize is the length of the signature (excluding null terminator).
	kFileSignatureSize = len(kFileSignature)
)

// kCurrentVersion is the current version of the deps_ log format.
const kCurrentVersion int32 = 4

// kMaxRecordSize is the maximum allowed size of a deps_ log record (in bytes).
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

func (d *Deps) GetMtime() int64 {
	return d.mtime
}
func (d *Deps) GetNodeCount() int {
	return d.node_count
}
func (d *Deps) GetNodes() []*Node {
	return d.nodes
}

type DepsLog struct {
	file_path_          string
	file_               *os.File
	deps_               []*Deps // 索引为节点 id_
	nodes_              []*Node // 节点 id_ -> Node
	needs_recompaction_ bool
}

func NewDepsLog(path string) *DepsLog {
	return &DepsLog{
		file_path_: path,
		deps_:      []*Deps{},
		nodes_:     []*Node{},
	}
}
func (dl *DepsLog) Close() error {
	dl.openForWriteIfNeeded()
	if dl.file_ != nil {
		err := dl.file_.Close()
		dl.file_ = nil
		return err
	}
	return nil
}

func (dl *DepsLog) OpenForWrite(path string, err *string) bool {
	if dl.needs_recompaction_ {
		if !dl.Recompact(path, err) {
			return false
		}
	}
	dl.file_path_ = path
	return true
}

func (dl *DepsLog) openForWriteIfNeeded() bool {
	if dl.file_ != nil || dl.file_path_ == "" {
		return true
	}
	f, err := os.OpenFile(dl.file_path_, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false
	}
	dl.file_ = f
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

	// Assign ids to all nodes_ that are missing one.
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

	// Update on-disk_interface_ representation.
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
	if err := binary.Write(d.file_, binary.LittleEndian, uint32(size)); err != nil {
		return false
	}
	// Write node id
	id := int32(node.id())
	if err := binary.Write(d.file_, binary.LittleEndian, id); err != nil {
		return false
	}
	// Write mtime low 32 bits
	mtimeLow := uint32(mtime & 0xffffffff)
	if err := binary.Write(d.file_, binary.LittleEndian, mtimeLow); err != nil {
		return false
	}
	// Write mtime high 32 bits
	mtimeHigh := uint32((mtime >> 32) & 0xffffffff)
	if err := binary.Write(d.file_, binary.LittleEndian, mtimeHigh); err != nil {
		return false
	}
	// Write each dependency node id
	for i := 0; i < nodeCount; i++ {
		id = int32(nodes[i].id())
		if err := binary.Write(d.file_, binary.LittleEndian, id); err != nil {
			return false
		}
	}
	// Flush
	if err := d.file_.Sync(); err != nil {
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
	path := node.path_
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
	if err := binary.Write(dl.file_, binary.LittleEndian, recordSize); err != nil {
		return false
	}
	if _, err := dl.file_.Write([]byte(path)); err != nil {
		return false
	}
	if _, err := dl.file_.Write([]byte{0}); err != nil {
		return false
	}
	if padding > 0 {
		if _, err := dl.file_.Write(make([]byte, padding)); err != nil {
			return false
		}
	}
	id := len(dl.nodes_)
	checksum := uint32(^id)
	if err := binary.Write(dl.file_, binary.LittleEndian, checksum); err != nil {
		return false
	}
	node.id_ = id
	dl.nodes_ = append(dl.nodes_, node)
	return true
}

// Load 从磁盘加载 .ninja_deps 文件
func (d *DepsLog) Load(path string, state *State, err *string) LoadStatus {
	// METRIC_RECORD(".ninja_deps load") - ignored

	f, errOpen := os.Open(path)
	if errOpen != nil {
		if os.IsNotExist(errOpen) {
			return LOAD_NOT_FOUND
		}
		*err = errOpen.Error()
		return LOAD_ERROR
	}
	//defer f.Close()

	// Read signature
	sigBuf := make([]byte, kFileSignatureSize)
	if _, errRead := f.Read(sigBuf); errRead != nil || !bytes.Equal(sigBuf, []byte(kFileSignature)) {
		// Invalid header
		f.Close()
		os.Remove(path)
		*err = "bad deps_ log signature or version; starting over"
		return LOAD_SUCCESS
	}

	// Read version
	var version int32
	if read_err := binary.Read(f, binary.LittleEndian, &version); read_err != nil || version != kCurrentVersion {
		f.Close()
		if version == 1 {
			*err = "deps_ log version change; rebuilding"
		} else {
			*err = "bad deps_ log signature or version; starting over"
		}
		os.Remove(path)
		return LOAD_SUCCESS
	}

	offset, _ := f.Seek(0, os.SEEK_CUR) // current log_file_ position after version
	readFailed := false
	uniqueDepRecordCount := 0
	totalDepRecordCount := 0

	buf := make([]byte, kMaxRecordSize+1)

	for {
		var size uint32
		if err := binary.Read(f, binary.LittleEndian, &size); err != nil {
			if err.Error() != "EOF" {
				readFailed = true
			}
			break
		}
		isDeps := (size >> 31) != 0
		size = size & 0x7FFFFFFF

		if size > kMaxRecordSize {
			readFailed = true
			break
		}
		if _, errRead := f.Read(buf[:size]); errRead != nil {
			readFailed = true
			break
		}
		// Update offset after reading this record
		offset += int64(4 + size)

		if isDeps {
			if size%4 != 0 {
				readFailed = true
				break
			}
			data := make([]int32, size/4)
			if err := binary.Read(bytes.NewReader(buf[:size]), binary.LittleEndian, &data); err != nil {
				readFailed = true
				break
			}
			outID := int(data[0])
			mtimeLow := uint64(uint32(data[1]))
			mtimeHigh := uint64(uint32(data[2]))
			mtime := int64((mtimeHigh << 32) | mtimeLow)
			depsCount := len(data) - 3

			// Validate node ids
			valid := true
			for i := 0; i < depsCount; i++ {
				nodeID := int(data[3+i])
				if nodeID >= len(d.nodes_) || d.nodes_[nodeID] == nil {
					valid = false
					break
				}
			}
			if !valid {
				readFailed = true
				break
			}

			deps := &Deps{mtime: mtime, node_count: depsCount, nodes: make([]*Node, depsCount)}
			for i := 0; i < depsCount; i++ {
				deps.nodes[i] = d.nodes_[int(data[3+i])]
			}
			totalDepRecordCount++
			if !d.UpdateDeps(outID, deps) {
				uniqueDepRecordCount++
			}
		} else {
			// Node record
			pathSize := int(size - 4)
			if pathSize <= 0 {
				readFailed = true
				break
			}
			// Trim up to 3 trailing null bytes (padding)
			for pathSize > 0 && buf[pathSize-1] == 0 {
				pathSize--
			}
			nodePath := string(buf[:pathSize])
			// Checksum is last 4 bytes of the record
			var checksum uint32
			if err := binary.Read(bytes.NewReader(buf[size-4:size]), binary.LittleEndian, &checksum); err != nil {
				readFailed = true
				break
			}
			expectedID := int(^checksum) // unary complement
			actualID := len(d.nodes_)
			if expectedID != actualID {
				readFailed = true
				break
			}
			node := state.GetNode(nodePath, 0) // slash_bits = 0
			if node.id() >= 0 {
				// Already has an id, conflict
				readFailed = true
				break
			}
			node.set_id(actualID)
			d.nodes_ = append(d.nodes_, node)
		}
	}

	if readFailed {
		// Determine error message
		var errMsg string
		if fErr := f.Close(); fErr != nil {
			errMsg = fErr.Error()
		} else {
			errMsg = "premature end of log_file_"
		}
		*err = errMsg

		// Truncate log_file_ to last known good offset
		if !Truncate(path, offset, err) {
			return LOAD_ERROR
		}
		*err += "; recovering"
		return LOAD_SUCCESS
	}
	f.Close()

	// Decide if recompaction is needed
	const kMinCompactionEntryCount = 1000
	const kCompactionRatio = 3
	if totalDepRecordCount > kMinCompactionEntryCount &&
		totalDepRecordCount > uniqueDepRecordCount*kCompactionRatio {
		d.needs_recompaction_ = true
	}

	return LOAD_SUCCESS
}

// GetDeps 获取输出节点的依赖
func (dl *DepsLog) GetDeps(out *Node) *Deps {
	if out.id_ < 0 || out.id_ >= len(dl.deps_) {
		return nil
	}
	return dl.deps_[out.id_]
}

// GetFirstReverseDepsNode 获取依赖某个节点的第一个输出节点
func (dl *DepsLog) GetFirstReverseDepsNode(node *Node) *Node {
	for id := 0; id < len(dl.deps_); id++ {
		deps := dl.deps_[id]
		if deps == nil {
			continue
		}
		for i := 0; i < deps.node_count; i++ {
			if deps.nodes[i] == node {
				return dl.nodes_[id]
			}
		}
	}
	return nil
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
	// 重置所有节点的 id_
	for _, n := range dl.nodes_ {
		if n != nil {
			n.id_ = -1
		}
	}
	// 重新记录所有存活的依赖
	for id, deps := range dl.deps_ {
		if deps == nil {
			continue
		}
		outNode := dl.nodes_[id]
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
	dl.deps_ = newLog.deps_
	dl.nodes_ = newLog.nodes_

	return ReplaceContent(path, temp_path, err)
}

// isDepsEntryLive 判断节点的依赖记录是否存活
func (dl *DepsLog) isDepsEntryLive(node *Node) bool {
	if node.in_edge() == nil {
		return false
	}
	// 检查边的规则是否有 "deps_" 属性
	return node.in_edge().GetBinding("deps_") != ""
}

func (dl *DepsLog) UpdateDeps(out_id int, deps *Deps) bool {
	if out_id >= len(dl.deps_) {
		dl.deps_ = append(dl.deps_, make([]*Deps, out_id+1-len(dl.deps_))...)
	}
	delete_old := dl.deps_[out_id] != nil
	//if delete_old {
	// slices.Delete(dl.deps_, out_id)
	//	}
	dl.deps_[out_id] = deps
	return delete_old
}

// / Used for tests.
func (dl *DepsLog) Nodes() []*Node { return dl.nodes_ }

func (dl *DepsLog) Deps() []*Deps { return dl.deps_ }

// IsDepsEntryLiveFor 判断节点的依赖记录是否应该保留。
// 节点必须有入边，且该边的 "deps_" 绑定非空。
func IsDepsEntryLiveFor(node *Node) bool {
	if node.in_edge() == nil {
		return false
	}
	return node.in_edge().GetBinding("deps_") != ""
}
func (d *DepsLog) OpenForWriteIfNeeded() bool {
	if d.file_path_ == "" {
		return true
	}

	var err error
	d.file_, err = os.OpenFile(d.file_path_, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return false
	}

	// Set buffer size to kMaxRecordSize+1 (simulate setvbuf)
	// In Go we can't set buffer on os.File directly; we rely on Sync after writes.
	// For consistency, we'll just use the log_file_ as is.

	// Set close-on-exec
	if fd := d.file_.Fd(); fd > 0 {
		syscall.CloseOnExec(syscall.Handle(fd))
	}

	// In append mode, log_file_ pointer is already at end; but ensure we're at end.
	if _, err := d.file_.Seek(0, os.SEEK_END); err != nil {
		return false
	}

	// Check if log_file_ is empty
	stat, err := d.file_.Stat()
	if err != nil {
		return false
	}
	if stat.Size() == 0 {
		// Write signature
		if _, err := d.file_.Write([]byte(kFileSignature)); err != nil {
			return false
		}
		// Write version
		if err := binary.Write(d.file_, binary.LittleEndian, kCurrentVersion); err != nil {
			return false
		}
	}

	// Flush
	if err := d.file_.Sync(); err != nil {
		return false
	}

	d.file_path_ = ""
	return true
}
