package builder

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
)

const (
	kSignature      = "# ninjadeps\n"
	kCurrentVersion = 4
	kMaxRecordSize  = (1 << 19) - 1
)

// Deps 表示一个输出的依赖列表
type Deps struct {
	Mtime int64
	Nodes []*Node
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

func (dl *DepsLog) OpenForWrite(path string) error {
	if dl.needsRecompaction {
		if err := dl.Recompact(path); err != nil {
			return err
		}
	}
	dl.filePath = path
	return nil
}

func (dl *DepsLog) openForWriteIfNeeded() error {
	if dl.file != nil || dl.filePath == "" {
		return nil
	}
	f, err := os.OpenFile(dl.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	dl.file = f
	// 检查文件是否为空
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

// RecordDeps 记录输出节点的依赖
func (dl *DepsLog) RecordDeps(out *Node, mtime int64, deps []*Node) error {
	// 检查是否有变化
	madeChange := false
	if out.ID < 0 {
		if err := dl.recordID(out); err != nil {
			return err
		}
		madeChange = true
	}
	for _, dep := range deps {
		if dep.ID < 0 {
			if err := dl.recordID(dep); err != nil {
				return err
			}
			madeChange = true
		}
	}
	if !madeChange {
		existing := dl.GetDeps(out)
		if existing != nil && existing.Mtime == mtime && len(existing.Nodes) == len(deps) {
			same := true
			for i, n := range existing.Nodes {
				if n != deps[i] {
					same = false
					break
				}
			}
			if same {
				return nil
			}
		}
	}

	// 计算记录大小
	size := 4 * (1 + 2 + len(deps)) // outID(4) + mtime低4 + mtime高4 + 每个依赖4
	if size > kMaxRecordSize {
		return fmt.Errorf("record too large")
	}
	if err := dl.openForWriteIfNeeded(); err != nil {
		return err
	}
	// 写入记录，最高位设为1表示依赖记录
	recordSize := uint32(size) | 0x80000000
	if err := binary.Write(dl.file, binary.LittleEndian, recordSize); err != nil {
		return err
	}
	if err := binary.Write(dl.file, binary.LittleEndian, int32(out.ID)); err != nil {
		return err
	}
	mtimeLow := uint32(mtime & 0xFFFFFFFF)
	mtimeHigh := uint32((mtime >> 32) & 0xFFFFFFFF)
	if err := binary.Write(dl.file, binary.LittleEndian, mtimeLow); err != nil {
		return err
	}
	if err := binary.Write(dl.file, binary.LittleEndian, mtimeHigh); err != nil {
		return err
	}
	for _, dep := range deps {
		if err := binary.Write(dl.file, binary.LittleEndian, int32(dep.ID)); err != nil {
			return err
		}
	}
	if err := dl.file.Sync(); err != nil {
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
func (dl *DepsLog) recordID(node *Node) error {
	if node.ID >= 0 {
		return nil
	}
	path := node.Path
	pathSize := len(path)
	padding := (4 - (pathSize+1)%4) % 4
	size := 4 + pathSize + 1 + padding // 4字节校验和 + 路径 + null + 填充
	if size > kMaxRecordSize {
		return fmt.Errorf("node path too long")
	}
	if err := dl.openForWriteIfNeeded(); err != nil {
		return err
	}
	// 写入节点记录（最高位为0）
	recordSize := uint32(size)
	if err := binary.Write(dl.file, binary.LittleEndian, recordSize); err != nil {
		return err
	}
	if _, err := dl.file.Write([]byte(path)); err != nil {
		return err
	}
	if _, err := dl.file.Write([]byte{0}); err != nil {
		return err
	}
	if padding > 0 {
		if _, err := dl.file.Write(make([]byte, padding)); err != nil {
			return err
		}
	}
	id := len(dl.nodes)
	checksum := uint32(^id)
	if err := binary.Write(dl.file, binary.LittleEndian, checksum); err != nil {
		return err
	}
	node.ID = id
	dl.nodes = append(dl.nodes, node)
	return nil
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
	var sig [len(kSignature)]byte
	if _, err := f.Read(sig[:]); err != nil {
		return err
	}
	if string(sig[:]) != kSignature {
		// 无效签名，删除文件并视为空
		f.Close()
		os.Remove(dl.filePath)
		return nil
	}
	var version int32
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
	offset := int64(len(kSignature) + 4) // 已读字节数
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
				idx := outNode.ID
				dl.ensureCapacity(idx)
				dl.deps[idx] = &Deps{Mtime: mtime, Nodes: deps}
				dl.nodes[idx] = outNode
				for _, dep := range deps {
					dl.reverseDeps[dep.ID] = append(dl.reverseDeps[dep.ID], idx)
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
			if node.ID >= 0 {
				// 节点已有 ID，冲突
				break
			}
			node.ID = expectedID
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
	if out.ID < 0 || out.ID >= len(dl.deps) {
		return nil
	}
	return dl.deps[out.ID]
}

// GetFirstReverseDepsNode 获取依赖某个节点的第一个输出节点
func (dl *DepsLog) GetFirstReverseDepsNode(dep *Node) *Node {
	revs, ok := dl.reverseDeps[dep.ID]
	if !ok || len(revs) == 0 {
		return nil
	}
	return dl.nodes[revs[0]]
}

// Recompact 重新压实日志
func (dl *DepsLog) Recompact(path string) error {
	dl.Close()
	tempPath := path + ".recompact"
	os.Remove(tempPath)

	newLog := NewDepsLog(tempPath)
	if err := newLog.OpenForWrite(tempPath); err != nil {
		return err
	}
	// 重置所有节点的 ID
	for _, n := range dl.nodes {
		if n != nil {
			n.ID = -1
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
		if err := newLog.RecordDeps(outNode, deps.Mtime, deps.Nodes); err != nil {
			newLog.Close()
			return err
		}
	}
	if err := newLog.Close(); err != nil {
		return err
	}
	// 替换文件
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	// 替换内存数据
	dl.deps = newLog.deps
	dl.nodes = newLog.nodes
	dl.reverseDeps = newLog.reverseDeps
	dl.needsRecompaction = false
	return nil
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
