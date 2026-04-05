package depslog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"ninja-go/pkg/graph"
	"os"
)

const (
	kSignature      = "# ninjadeps\n"
	kCurrentVersion = 4
	kMaxRecordSize  = (1 << 19) - 1
)

// Deps 表示一个输出的依赖列表
type Deps struct {
	Mtime int64
	Nodes []*graph.Node
}

type DepsLog struct {
	file string
	// cache       map[string][]string // output path -> list of implicit dep paths
	deps           []*Deps       // 索引为节点 ID
	nodes          []*graph.Node // 节点 ID -> Node
	reverseDeps    map[int][]int // 依赖节点 ID -> 被依赖的输出节点 ID 列表（反向依赖）
	fileHandle     *os.File
	needsRecompact bool
}

func NewDepsLog(path string) *DepsLog {
	return &DepsLog{
		file: path,
		// cache:       make(map[string][]string),
		deps:        []*Deps{},
		nodes:       []*graph.Node{},
		reverseDeps: make(map[int][]int),
	}
}

type tempRecord struct {
	isDeps bool
	data   []byte
}

func (dl *DepsLog) Load(state *graph.State) error {
	f, err := os.Open(dl.file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	// 读取签名和版本
	var signature [len(kSignature)]byte
	if _, err := f.Read(signature[:]); err != nil {
		return err
	}
	if string(signature[:]) != kSignature {
		return fmt.Errorf("bad deps log signature")
	}
	var version int32
	if err := binary.Read(f, binary.LittleEndian, &version); err != nil {
		return err
	}
	if version != kCurrentVersion {
		return fmt.Errorf("unsupported version %d", version)
	}

	// 读取所有记录到内存
	var records []tempRecord
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
			return fmt.Errorf("record too large: %d", size)
		}
		data := make([]byte, size)
		if _, err := f.Read(data); err != nil {
			return err
		}
		records = append(records, tempRecord{isDeps: isDeps, data: data})
	}

	// 重置内部状态
	dl.deps = []*Deps{}
	dl.nodes = []*graph.Node{}
	dl.reverseDeps = make(map[int][]int)

	// 第一遍：处理节点记录，建立 ID -> Node 映射
	for _, rec := range records {
		if rec.isDeps {
			continue
		}
		r := bytes.NewReader(rec.data)
		var pathBytes []byte
		for {
			b, err := r.ReadByte()
			if err != nil {
				return err
			}
			if b == 0 {
				break
			}
			pathBytes = append(pathBytes, b)
		}
		path := string(pathBytes)
		// 跳过填充
		padding := (4 - (len(path)+1)%4) % 4
		if _, err := r.Seek(int64(padding), os.SEEK_CUR); err != nil {
			return err
		}
		var checksum uint32
		if err := binary.Read(r, binary.LittleEndian, &checksum); err != nil {
			return err
		}
		expectedID := int(^checksum)
		node := state.GetNode(path)
		if node.ID >= 0 && node.ID != expectedID {
			return fmt.Errorf("node %s already has id %d, expected %d", path, node.ID, expectedID)
		}
		node.ID = expectedID
		dl.ensureCapacity(expectedID)
		dl.nodes[expectedID] = node
	}

	// 第二遍：处理依赖记录
	for _, rec := range records {
		if !rec.isDeps {
			continue
		}
		r := bytes.NewReader(rec.data)
		var outID int32
		if err := binary.Read(r, binary.LittleEndian, &outID); err != nil {
			return err
		}
		if int(outID) >= len(dl.nodes) || dl.nodes[outID] == nil {
			return fmt.Errorf("unknown node id %d", outID)
		}
		outNode := dl.nodes[outID]
		var mtimeLow, mtimeHigh uint32
		if err := binary.Read(r, binary.LittleEndian, &mtimeLow); err != nil {
			return err
		}
		if err := binary.Read(r, binary.LittleEndian, &mtimeHigh); err != nil {
			return err
		}
		mtime := int64(mtimeHigh)<<32 | int64(mtimeLow)
		depCount := (len(rec.data) - 12) / 4
		depIDs := make([]int32, depCount)
		for i := 0; i < int(depCount); i++ {
			if err := binary.Read(r, binary.LittleEndian, &depIDs[i]); err != nil {
				return err
			}
		}
		var deps []*graph.Node
		for _, id := range depIDs {
			if int(id) >= len(dl.nodes) || dl.nodes[id] == nil {
				return fmt.Errorf("unknown dep node id %d", id)
			}
			deps = append(deps, dl.nodes[id])
		}
		idx := outNode.ID
		dl.ensureCapacity(idx)
		dl.deps[idx] = &Deps{Mtime: mtime, Nodes: deps}
		dl.nodes[idx] = outNode
		for _, dep := range deps {
			dl.reverseDeps[dep.ID] = append(dl.reverseDeps[dep.ID], idx)
		}
	}

	return nil
}

// OpenForWrite 准备写入日志（通常 Load 后调用）
func (dl *DepsLog) OpenForWrite() error {
	if dl.fileHandle != nil {
		return nil
	}
	f, err := os.OpenFile(dl.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	dl.fileHandle = f
	// 如果文件为空，写入签名和版本
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		if _, err := f.Write([]byte(kSignature)); err != nil {
			return err
		}
		if err := binary.Write(f, binary.LittleEndian, int32(kCurrentVersion)); err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭并保存日志
func (dl *DepsLog) Close() error {
	if dl.fileHandle != nil {
		return dl.fileHandle.Close()
	}
	return nil
}

// Save 将缓存写入磁盘
//func (dl *DepsLog) Save() error {
//	f, err := os.Create(dl.file)
//	if err != nil {
//		return err
//	}
//	defer f.Close()
//	// 写入签名
//	if _, err := f.Write([]byte(kSignature)); err != nil {
//		return err
//	}
//	// 写入版本
//	if err := binary.Write(f, binary.LittleEndian, uint32(kCurrentVersion)); err != nil {
//		return err
//	}
//	for outID, deps := range dl.deps {
//		if deps == nil {
//			continue
//		}
//		outNode := dl.nodes[outID]
//		if outNode == nil {
//			continue
//		}
//		// 记录大小（4字节）+ 输出节点名（含 null）+ mtime + 依赖数量 + 依赖ID列表
//		nameBytes := []byte(outNode.Path)
//		nameBytes = append(nameBytes, 0)
//		size := 4 + len(nameBytes) + 4 + 4 + len(deps.Nodes)*4
//		if err := binary.Write(f, binary.LittleEndian, uint32(size)); err != nil {
//			return err
//		}
//		if _, err := f.Write(nameBytes); err != nil {
//			return err
//		}
//		if err := binary.Write(f, binary.LittleEndian, deps.Mtime); err != nil {
//			return err
//		}
//		if err := binary.Write(f, binary.LittleEndian, uint32(len(deps.Nodes))); err != nil {
//			return err
//		}
//		for _, dep := range deps.Nodes {
//			if err := binary.Write(f, binary.LittleEndian, uint32(dep.ID)); err != nil {
//				return err
//			}
//		}
//	}
//	return nil
//}

// RecordDeps 记录某个输出节点的依赖
func (dl *DepsLog) RecordDeps(out *graph.Node, mtime int64, deps []*graph.Node) error {
	if err := dl.OpenForWrite(); err != nil {
		return err
	}
	// 确保所有节点都有 ID
	if out.ID < 0 {
		if err := dl.recordID(out); err != nil {
			return err
		}
	}
	for _, dep := range deps {
		if dep.ID < 0 {
			if err := dl.recordID(dep); err != nil {
				return err
			}
		}
	}
	// 计算记录大小：outID(4) + mtime(8) + len(deps)*4
	size := 4 + 8 + len(deps)*4
	if size > kMaxRecordSize {
		return fmt.Errorf("record too large: %d", size)
	}
	// 写入记录（最高位设为1表示deps记录）
	recordSize := uint32(size) | 0x80000000
	if err := binary.Write(dl.fileHandle, binary.LittleEndian, recordSize); err != nil {
		return err
	}
	if err := binary.Write(dl.fileHandle, binary.LittleEndian, int32(out.ID)); err != nil {
		return err
	}
	mtimeLow := uint32(mtime & 0xFFFFFFFF)
	mtimeHigh := uint32((mtime >> 32) & 0xFFFFFFFF)
	if err := binary.Write(dl.fileHandle, binary.LittleEndian, mtimeLow); err != nil {
		return err
	}
	if err := binary.Write(dl.fileHandle, binary.LittleEndian, mtimeHigh); err != nil {
		return err
	}
	for _, dep := range deps {
		if err := binary.Write(dl.fileHandle, binary.LittleEndian, int32(dep.ID)); err != nil {
			return err
		}
	}
	if err := dl.fileHandle.Sync(); err != nil {
		return err
	}
	// 更新内存
	idx := out.ID
	dl.ensureCapacity(idx)
	dl.deps[idx] = &Deps{Mtime: mtime, Nodes: deps}
	dl.nodes[idx] = out
	for _, dep := range deps {
		dl.reverseDeps[dep.ID] = append(dl.reverseDeps[dep.ID], idx)
	}
	return nil
}

// recordID 为节点分配 ID 并写入节点记录
func (dl *DepsLog) recordID(node *graph.Node) error {
	if node.ID >= 0 {
		return nil // 已经有 ID
	}
	path := node.Path
	pathSize := len(path)
	padding := (4 - (pathSize+1)%4) % 4
	size := 4 + pathSize + 1 + padding // 4字节校验和 + 路径 + null + 填充
	if size > kMaxRecordSize {
		return fmt.Errorf("node path too long")
	}
	// 写入节点记录（最高位为0）
	recordSize := uint32(size)
	if err := binary.Write(dl.fileHandle, binary.LittleEndian, recordSize); err != nil {
		return err
	}
	if _, err := dl.fileHandle.Write([]byte(path)); err != nil {
		return err
	}
	if _, err := dl.fileHandle.Write([]byte{0}); err != nil {
		return err
	}
	// 写入填充
	if padding > 0 {
		if _, err := dl.fileHandle.Write(make([]byte, padding)); err != nil {
			return err
		}
	}
	id := len(dl.nodes)
	checksum := uint32(^id)
	if err := binary.Write(dl.fileHandle, binary.LittleEndian, checksum); err != nil {
		return err
	}
	node.ID = id
	dl.nodes = append(dl.nodes, node)
	return nil
}

// AddDeps 记录某个输出文件的隐式依赖
//func (dl *DepsLog) AddDeps(output string, deps []string) {
//	dl.mu.Lock()
//	defer dl.mu.Unlock()
//	// dl.cache[output] = deps
//}

// AddDeps 记录输出文件的隐式依赖（需要节点对象，如果节点不存在则创建）
//func (dl *DepsLog) AddDeps(output string, deps []string, state *graph.State) {
//	// dl.cache[output] = deps
//	outNode := state.GetNode(output)
//	var depNodes []*graph.Node
//	for _, d := range deps {
//		depNodes = append(depNodes, state.GetNode(d))
//	}
//	dl.RecordDeps(outNode, 0, depNodes)
//}

// GetDepsSimple 获取简单缓存的依赖（用于兼容）
//func (dl *DepsLog) GetDepsSimple(output string) []string {
//	dl.mu.RLock()
//	defer dl.mu.RUnlock()
//	return dl.cache[output]
//}

// GetDeps 获取之前记录的隐式依赖
//func (dl *DepsLog) GetDeps(output string) []string {
//	dl.mu.RLock()
//	defer dl.mu.RUnlock()
//	return dl.cache[output]
//}

// GetDeps 获取输出节点的依赖
func (dl *DepsLog) GetDeps(out *graph.Node) *Deps {
	if out.ID < 0 || out.ID >= len(dl.deps) {
		return nil
	}
	return dl.deps[out.ID]
}

// GetFirstReverseDepsNode 获取依赖某个节点的第一个输出节点（用于反向依赖链表）
func (dl *DepsLog) GetFirstReverseDepsNode(dep *graph.Node) *graph.Node {
	revs := dl.reverseDeps[dep.ID]
	if len(revs) == 0 {
		return nil
	}
	return dl.nodes[revs[0]]
}

// Recompact 重新压实日志：删除那些输出节点已死亡或依赖列表为空的记录
func (dl *DepsLog) Recompact() error {
	// 关闭当前文件
	dl.Close()
	tempPath := dl.file + ".recompact"
	// 删除可能残留的临时文件
	os.Remove(tempPath)

	newLog := NewDepsLog(tempPath)
	if err := newLog.OpenForWrite(); err != nil {
		return err
	}

	// 遍历所有输出节点（索引即为节点 ID）
	for id, deps := range dl.deps {
		if deps == nil {
			continue
		}
		outNode := dl.nodes[id]
		if outNode == nil {
			continue
		}
		// 判断该节点的依赖记录是否存活
		if !dl.isDepsEntryLive(outNode) {
			continue
		}
		// 重新记录依赖
		if err := newLog.RecordDeps(outNode, deps.Mtime, deps.Nodes); err != nil {
			newLog.Close()
			return err
		}
	}
	if err := newLog.Close(); err != nil {
		return err
	}

	// 替换原文件
	if err := os.Rename(tempPath, dl.file); err != nil {
		return err
	}

	// 将新日志的数据替换当前日志的数据
	dl.deps = newLog.deps
	dl.nodes = newLog.nodes
	dl.reverseDeps = newLog.reverseDeps
	return nil
}

// isDepsEntryLive 判断节点的依赖记录是否应该保留
func (dl *DepsLog) isDepsEntryLive(node *graph.Node) bool {
	if node.Edge == nil {
		return false
	}
	// 检查边的规则是否有 "deps" 属性（如 deps = gcc 或 deps = msvc）
	if node.Edge.Rule == nil {
		return false
	}
	// 如果规则有 Deps 字段（字符串）且非空，则认为存活
	return node.Edge.Rule.Depfile != ""
}

func (dl *DepsLog) ensureCapacity(id int) {
	if id >= len(dl.deps) {
		newDeps := make([]*Deps, id+1)
		copy(newDeps, dl.deps)
		dl.deps = newDeps
	}
	if id >= len(dl.nodes) {
		newNodes := make([]*graph.Node, id+1)
		copy(newNodes, dl.nodes)
		dl.nodes = newNodes
	}

	//if id >= len(dl.deps) {
	//	dl.deps = append(dl.deps, make([]*Deps, id+1-len(dl.deps))...)
	//	dl.nodes = append(dl.nodes, make([]*graph.Node, id+1-len(dl.nodes))...)
	//}
}

// GetReverseDeps 返回所有依赖给定节点的输出节点列表（即该节点作为输入被哪些输出节点依赖）
func (dl *DepsLog) GetReverseDeps(dep *graph.Node) []*graph.Node {
	revIDs := dl.reverseDeps[dep.ID]
	result := make([]*graph.Node, 0, len(revIDs))
	for _, id := range revIDs {
		if id < len(dl.nodes) {
			result = append(result, dl.nodes[id])
		}
	}
	return result
}
