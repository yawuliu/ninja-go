package depslog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"ninja-go/pkg/graph"
	"os"
	"sync"
)

const (
	kSignature      = "# ninjadeps\n"
	kCurrentVersion = 4
)

// Deps 表示一个输出的依赖列表
type Deps struct {
	Mtime int64
	Nodes []*graph.Node
}

type DepsLog struct {
	mu   sync.RWMutex
	file string
	// cache       map[string][]string // output path -> list of implicit dep paths
	deps        []*Deps       // 索引为节点 ID
	nodes       []*graph.Node // 节点 ID -> Node
	reverseDeps map[int][]int // 依赖节点 ID -> 被依赖的输出节点 ID 列表（反向依赖）
	open        bool
	dirty       bool
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

// Load 从磁盘读取 .ninja_deps
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
	var version uint32
	if err := binary.Read(f, binary.LittleEndian, &version); err != nil {
		return err
	}
	if version != kCurrentVersion {
		return fmt.Errorf("unsupported version %d", version)
	}

	dl.mu.Lock()
	defer dl.mu.Unlock()
	dl.deps = []*Deps{}
	dl.nodes = []*graph.Node{}
	dl.reverseDeps = make(map[int][]int)

	for {
		// 读取记录大小
		var size uint32
		if err := binary.Read(f, binary.LittleEndian, &size); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return err
		}
		if size < 4 {
			return fmt.Errorf("invalid record size %d", size)
		}
		// 读取整个记录内容
		record := make([]byte, size)
		if _, err := f.Read(record); err != nil {
			return err
		}
		// 解析记录内容
		r := bytes.NewReader(record)
		// 读取输出节点名称（以 null 结尾）
		// 读取输出节点名称（以 null 结尾）
		var nameBytes []byte
		for {
			b, err := r.ReadByte()
			if err != nil {
				return err
			}
			if b == 0 {
				break
			}
			nameBytes = append(nameBytes, b)
		}
		outputPath := string(nameBytes)
		// 读取 mtime
		var mtime int64
		if err := binary.Read(r, binary.LittleEndian, &mtime); err != nil {
			return err
		}
		// 读取依赖数量
		var depCount uint32
		if err := binary.Read(r, binary.LittleEndian, &depCount); err != nil {
			return err
		}
		// 读取依赖节点 ID 列表
		depIDs := make([]uint32, depCount)
		for i := 0; i < int(depCount); i++ {
			if err := binary.Read(r, binary.LittleEndian, &depIDs[i]); err != nil {
				return err
			}
		}
		// 可选：检查是否已读完整个记录（r.Len() == 0）
		outNode := state.AddNode(outputPath)
		idx := outNode.ID
		dl.ensureNodeCapacity(idx)
		var deps []*graph.Node
		for _, id := range depIDs {
			depNode := state.GetNodeByID(int(id))
			if depNode == nil {
				// 理论上不应发生，但若缺失则创建占位节点（分配新ID，但会破坏一致性）
				depNode = state.AddNode(fmt.Sprintf("missing-%d", id))
			}
			deps = append(deps, depNode)
		}
		dl.deps[idx] = &Deps{Mtime: mtime, Nodes: deps}
		dl.nodes[idx] = outNode
		for _, dep := range deps {
			dl.reverseDeps[dep.ID] = append(dl.reverseDeps[dep.ID], idx)
		}
		//// 获取或创建输出节点
		//outNode := state.GetNode(outputPath)
		//// 记录依赖节点（按 ID 获取，若节点不存在则创建）
		//var deps []*graph.Node
		//for _, id := range depIDs {
		//	n := dl.getNodeByID(int(id))
		//	if n == nil {
		//		// 节点可能在 state 中不存在，创建它
		//		n = state.GetNode(fmt.Sprintf("depsnode-%d", id)) // 实际应解析路径，这里简化
		//	}
		//	deps = append(deps, n)
		//}
		//// 存储到 deps 列表（索引为输出节点的 ID）
		//idx := outNode.ID
		//if idx >= len(dl.deps) {
		//	dl.deps = append(dl.deps, make([]*Deps, idx+1-len(dl.deps))...)
		//}
		//dl.deps[idx] = &Deps{Mtime: int(mtime), Nodes: deps}
		//dl.ensureNodeCapacity(idx)
		//dl.nodes[idx] = outNode
		//// 建立反向依赖
		//for _, dep := range deps {
		//	dl.reverseDeps[dep.ID] = append(dl.reverseDeps[dep.ID], idx)
		//}
	}
	return nil
	//dec := gob.NewDecoder(f)
	//return dec.Decode(&dl.cache)
}

// OpenForWrite 准备写入日志（通常 Load 后调用）
func (dl *DepsLog) OpenForWrite() error {
	if dl.open {
		return nil
	}
	dl.open = true
	dl.dirty = false
	return nil
}

// Close 关闭并保存日志
func (dl *DepsLog) Close() error {
	if !dl.open {
		return nil
	}
	defer func() { dl.open = false }()
	if dl.dirty {
		return dl.Save()
	}
	return nil
}

// Save 将缓存写入磁盘
func (dl *DepsLog) Save() error {
	f, err := os.Create(dl.file)
	if err != nil {
		return err
	}
	defer f.Close()
	// 写入签名
	if _, err := f.Write([]byte(kSignature)); err != nil {
		return err
	}
	// 写入版本
	if err := binary.Write(f, binary.LittleEndian, uint32(kCurrentVersion)); err != nil {
		return err
	}
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	for outID, deps := range dl.deps {
		if deps == nil {
			continue
		}
		outNode := dl.nodes[outID]
		if outNode == nil {
			continue
		}
		// 记录大小（4字节）+ 输出节点名（含 null）+ mtime + 依赖数量 + 依赖ID列表
		nameBytes := []byte(outNode.Path)
		nameBytes = append(nameBytes, 0)
		size := 4 + len(nameBytes) + 4 + 4 + len(deps.Nodes)*4
		if err := binary.Write(f, binary.LittleEndian, uint32(size)); err != nil {
			return err
		}
		if _, err := f.Write(nameBytes); err != nil {
			return err
		}
		if err := binary.Write(f, binary.LittleEndian, deps.Mtime); err != nil {
			return err
		}
		if err := binary.Write(f, binary.LittleEndian, uint32(len(deps.Nodes))); err != nil {
			return err
		}
		for _, dep := range deps.Nodes {
			if err := binary.Write(f, binary.LittleEndian, uint32(dep.ID)); err != nil {
				return err
			}
		}
	}
	return nil
	//dl.mu.RLock()
	//defer dl.mu.RUnlock()
	//f, err := os.Create(dl.file)
	//if err != nil {
	//	return err
	//}
	//defer f.Close()
	//enc := gob.NewEncoder(f)
	//return enc.Encode(dl.cache)
}

// RecordDeps 记录某个输出节点的依赖
func (dl *DepsLog) RecordDeps(out *graph.Node, mtime int64, deps []*graph.Node) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	idx := out.ID
	dl.ensureNodeCapacity(idx)
	dl.deps[idx] = &Deps{Mtime: mtime, Nodes: deps}
	dl.nodes[idx] = out
	// 更新反向依赖
	for _, dep := range deps {
		dl.reverseDeps[dep.ID] = append(dl.reverseDeps[dep.ID], idx)
	}
	dl.dirty = true
}

// AddDeps 记录某个输出文件的隐式依赖
//func (dl *DepsLog) AddDeps(output string, deps []string) {
//	dl.mu.Lock()
//	defer dl.mu.Unlock()
//	// dl.cache[output] = deps
//}

// AddDeps 记录输出文件的隐式依赖（需要节点对象，如果节点不存在则创建）
func (dl *DepsLog) AddDeps(output string, deps []string, state *graph.State) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	// dl.cache[output] = deps
	outNode := state.GetNode(output)
	var depNodes []*graph.Node
	for _, d := range deps {
		depNodes = append(depNodes, state.GetNode(d))
	}
	dl.RecordDeps(outNode, 0, depNodes)
}

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
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	if out.ID < len(dl.deps) {
		return dl.deps[out.ID]
	}
	return nil
}

// GetFirstReverseDepsNode 获取依赖某个节点的第一个输出节点（用于反向依赖链表）
func (dl *DepsLog) GetFirstReverseDepsNode(dep *graph.Node) *graph.Node {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	if revs, ok := dl.reverseDeps[dep.ID]; ok && len(revs) > 0 {
		return dl.nodes[revs[0]]
	}
	return nil
}

// Recompact 重新压实日志：删除那些输出节点已死亡或依赖列表为空的记录
func (dl *DepsLog) Recompact() error {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	// 遍历所有记录，保留那些输出节点仍然存在且有依赖的记录
	newDeps := make([]*Deps, len(dl.deps))
	newNodes := make([]*graph.Node, len(dl.nodes))
	// 重建反向依赖
	newReverse := make(map[int][]int)
	for i, deps := range dl.deps {
		if deps == nil {
			continue
		}
		outNode := dl.nodes[i]
		if outNode == nil || outNode.Edge == nil { // 节点不再有生成边，视为死亡
			continue
		}
		newDeps[i] = deps
		newNodes[i] = outNode
		for _, dep := range deps.Nodes {
			newReverse[dep.ID] = append(newReverse[dep.ID], i)
		}
	}
	dl.deps = newDeps
	dl.nodes = newNodes
	dl.reverseDeps = newReverse
	dl.dirty = true
	return nil
}

func (dl *DepsLog) ensureNodeCapacity(id int) {
	if id >= len(dl.deps) {
		dl.deps = append(dl.deps, make([]*Deps, id+1-len(dl.deps))...)
		dl.nodes = append(dl.nodes, make([]*graph.Node, id+1-len(dl.nodes))...)
	}
}

// GetReverseDeps 返回所有依赖给定节点的输出节点列表（即该节点作为输入被哪些输出节点依赖）
func (dl *DepsLog) GetReverseDeps(dep *graph.Node) []*graph.Node {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	revIDs, ok := dl.reverseDeps[dep.ID]
	if !ok {
		return nil
	}
	result := make([]*graph.Node, 0, len(revIDs))
	for _, id := range revIDs {
		if id < len(dl.nodes) {
			result = append(result, dl.nodes[id])
		}
	}
	return result
}
