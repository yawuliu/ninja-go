package buildlog

import (
	"bufio"
	"fmt"
	"ninja-go/pkg/graph"
	"ninja-go/pkg/util"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	LogVersion         = 5
	RecompactThreshold = 1000 // 记录数超过此值触发 recompact
)

// DiskInterface 用于获取文件状态（便于测试）
type DiskInterface interface {
	Stat(path string) (os.FileInfo, error)
}

// BuildLogUser 接口，用于判断路径是否已死（不再需要构建）
type BuildLogUser interface {
	IsPathDead(path string) bool
}

type Record struct {
	Output      string
	CommandHash string
	StartTime   int64
	EndTime     int64
	Mtime       int64 // 文件修改时间，用于 restat
}

type BuildLog struct {
	mu                 sync.RWMutex
	file               string
	records            map[string]*Record
	user               BuildLogUser
	dirty              bool      // 是否需要重新写入
	open               bool      // 是否已打开写入
	tempFile           util.File // 写入时的临时文件
	recompactThreshold int       // 触发 recompact 的冗余记录比例（默认 2）
}

func NewBuildLog(path string) *BuildLog {
	return &BuildLog{
		file:               path,
		records:            make(map[string]*Record),
		recompactThreshold: 2, // 当总记录数超过活跃记录数 * threshold 时 recompact
	}
}

func (bl *BuildLog) Load(fs util.FileSystem) error {
	return bl.LoadWithUser(fs, nil)
}

// LoadWithUser 加载日志，同时设置 user 用于后续 recompact
func (bl *BuildLog) LoadWithUser(fs util.FileSystem, user BuildLogUser) error {
	bl.user = user
	return bl.loadLocked(fs)
}

func (bl *BuildLog) loadLocked(fs util.FileSystem) error {
	f, err := fs.Open(bl.file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	bl.mu.Lock()
	defer bl.mu.Unlock()
	bl.records = make(map[string]*Record) // 清空旧记录

	scanner := bufio.NewScanner(f)
	// 设置缓冲区大小以处理超长行（默认 64KB，增大到 1MB）
	// scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	const maxScanTokenSize = 1024 * 1024
	scanner.Buffer(make([]byte, maxScanTokenSize), maxScanTokenSize)

	var version int
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// 处理版本头
		if strings.HasPrefix(line, "# ninja log v") {
			vStr := strings.TrimPrefix(line, "# ninja log v")
			if v, err := strconv.Atoi(vStr); err == nil {
				version = v
			}
			// 如果已经读过版本头，忽略后续的（防重复）
			continue
		}
		// 忽略非版本头的注释
		if strings.HasPrefix(line, "#") {
			continue
		}
		// 检查版本是否支持
		if version < 5 && version != 0 {
			return fmt.Errorf("unsupported log version %d", version)
		}
		// 解析记录：格式 "start end mtime command_hash output"
		// 注意 C++ 版本有 5 个字段，但当前实现可能只有 4 个，我们兼容两种
		// fields := strings.Fields(line)
		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			continue // 格式错误，跳过
		}
		start, _ := strconv.ParseInt(fields[0], 10, 64)
		end, _ := strconv.ParseInt(fields[1], 10, 64)
		mtime, _ := strconv.ParseInt(fields[2], 10, 64)
		output := fields[3]
		hash := fields[4]
		//start, _ := strconv.ParseInt(fields[0], 10, 64)
		//end, _ := strconv.ParseInt(fields[1], 10, 64)
		//var mtime int64
		//var hash string
		//var output string
		//if len(fields) == 4 {
		//	// 旧格式: start end hash output
		//	hash = fields[2]
		//	output = fields[3]
		//	mtime = end // 兼容旧版本，将 end 视为 mtime
		//} else {
		//	// 新格式: start end mtime hash output
		//	mtime, _ = strconv.ParseInt(fields[2], 10, 64)
		//	hash = fields[3]
		//	output = fields[4]
		//}
		bl.records[output] = &Record{
			Output:      output,
			CommandHash: hash,
			StartTime:   start,
			EndTime:     end,
			Mtime:       mtime,
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	// 检查是否需要 recompact
	// if len(bl.records) > RecompactThreshold {
	// 	bl.dirty = true
	// }
	//if len(bl.records) > RecompactThreshold {
	bl.recompactLocked()
	//}
	return nil
}

func (bl *BuildLog) Save() error {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	f, err := os.Create(bl.file)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "# ninja log v%d\n", LogVersion)
	for _, rec := range bl.records {
		// fmt.Fprintf(f, "%d %d %s %s\n", rec.StartTime, rec.EndTime, rec.CommandHash, rec.Output)
		fmt.Fprintf(f, "%d\t%d\t%d\t%s\t%s\n", rec.StartTime, rec.EndTime, rec.Mtime, rec.Output, rec.CommandHash)
	}
	return nil
}

func (bl *BuildLog) saveLocked(fs util.FileSystem) error {
	// 如果 user 存在，执行 recompact
	if bl.user != nil { // && bl.dirty
		bl.recompactLocked()
	}
	f, err := fs.Create(bl.file)
	if err != nil {
		return err
	}
	defer f.Close()
	// 写入版本头
	fmt.Fprintf(f, "# ninja log v%d\n", LogVersion)
	for _, rec := range bl.records {
		fmt.Fprintf(f, "%d\t%d\t%d\t%s\t%s\n", rec.StartTime, rec.EndTime, rec.Mtime, rec.Output, rec.CommandHash)
	}
	// bl.dirty = false
	return nil
}

// recompactLocked 移除所有被 user 标记为死亡的记录
func (bl *BuildLog) recompactLocked() {
	if bl.user == nil {
		return
	}
	newRecords := make(map[string]*Record)
	for out, rec := range bl.records {
		if !bl.user.IsPathDead(out) {
			newRecords[out] = rec
		}
	}
	bl.records = newRecords
	// bl.dirty = false
}

// RecordCommand 记录一条边的构建信息（支持多输出）
func (bl *BuildLog) RecordCommand(edge *graph.Edge, start, end int64, mtime int64) {
	command := edge.EvaluateCommand(true) // 包含 rspfile 内容
	commandHash := graph.HashCommand(command)

	for _, out := range edge.Outputs {
		path := out.Path
		entry, ok := bl.entries[path]
		if !ok {
			entry = &LogEntry{Output: path}
			bl.entries[path] = entry
		}
		entry.CommandHash = commandHash
		entry.StartTime = start
		entry.EndTime = end
		entry.Mtime = mtime

		if bl.fileHandle != nil {
			if err := bl.WriteEntry(bl.fileHandle, entry); err != nil {
				return err
			}
			if err := bl.fileHandle.Sync(); err != nil {
				return err
			}
		}
	}
	return nil
}

// LookupByOutput 返回输出对应的记录（如果存在）
func (bl *BuildLog) LookupByOutput(output string) *Record {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	return bl.records[output]
}

// GetCommandHash 返回输出对应的命令哈希（兼容旧接口）
func (bl *BuildLog) GetCommandHash(output string) string {
	rec := bl.LookupByOutput(output)
	if rec == nil {
		return ""
	}
	return rec.CommandHash
}

// UpdateRecord 更新或添加一条记录（兼容旧接口）
func (bl *BuildLog) UpdateRecord(rec *Record) {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	bl.records[rec.Output] = rec
	// bl.dirty = true
}

// Restat 重新检查输出文件的时间戳，并更新日志中的 mtime。
// outputsToIgnore: 不需要更新 mtime 的输出列表（通常是因为 restat 规则中这些输出没有改变）
// Restat 更新记录中的 mtime 字段，并可选地忽略某些输出（例如 restat 规则）
// 参数 outputsToIgnore: 那些实际没有改变的输出，其 mtime 不会被更新（但此处语义是忽略它们？）
// 原 C++ 实现中 Restat 用于重新读取磁盘上的 mtime 并更新日志中的 mtime，同时过滤掉未改变的输出。
func (bl *BuildLog) Restat(disk DiskInterface, outputsToIgnore []string) error {
	ignoreSet := make(map[string]bool)
	for _, out := range outputsToIgnore {
		ignoreSet[out] = true
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()
	for _, rec := range bl.records {
		if ignoreSet[rec.Output] {
			continue
		}
		info, err := disk.Stat(rec.Output)
		if err != nil {
			if os.IsNotExist(err) {
				rec.Mtime = 0
			}
			continue
		}
		rec.Mtime = info.ModTime().UnixNano()
	}
	// bl.dirty = true
	return nil
}

// OpenForWrite 准备写入（可空实现，实际保存延迟到 Close）
func (bl *BuildLog) OpenForWrite(fs util.FileSystem) error {
	return bl.OpenForWriteWithUser(fs, nil)
}

// OpenForWriteWithUser 打开日志并设置 BuildLogUser
func (bl *BuildLog) OpenForWriteWithUser(fs util.FileSystem, user BuildLogUser) error {
	if bl.open {
		return nil
	}
	if err := bl.LoadWithUser(fs, user); err != nil {
		return err
	}
	// 创建临时文件
	tempName := bl.file + ".tmp"
	f, err := fs.Create(tempName)
	if err != nil {
		return err
	}
	bl.tempFile = f
	bl.open = true
	// bl.dirty = false
	// 写入版本头
	_, err = fmt.Fprintf(f, "# ninja log v%d\n", LogVersion)
	return err
}

// Close 关闭并保存
func (bl *BuildLog) Close() error {
	if !bl.open {
		return nil
	}
	defer func() {
		bl.open = false
		bl.tempFile = nil
	}()
	//if bl.dirty {
	// 写入所有记录
	if err := bl.writeRecords(); err != nil {
		return err
	}
	//}
	if err := bl.tempFile.Close(); err != nil {
		return err
	}
	// 替换原文件
	if err := os.Rename(bl.file+".tmp", bl.file); err != nil {
		return err
	}
	return nil
}

// writeRecords 将当前记录写入临时文件（会进行 recompact）
func (bl *BuildLog) writeRecords() error {
	if bl.tempFile == nil {
		return fmt.Errorf("buildlog: not open for write")
	}
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	// 判断是否需要 recompact
	needRecompact := false
	if bl.user != nil {
		total := len(bl.records)
		alive := 0
		for out := range bl.records {
			if !bl.user.IsPathDead(out) {
				alive++
			}
		}
		if total > alive*bl.recompactThreshold {
			needRecompact = true
		}
	}
	// 写入临时文件
	for _, rec := range bl.records {
		if needRecompact && bl.user != nil && bl.user.IsPathDead(rec.Output) {
			continue // 跳过死亡记录
		}
		//line := fmt.Sprintf("%d\t%d\t%s\t%s", rec.StartTime, rec.EndTime, rec.CommandHash, rec.Output)
		//if rec.Mtime != 0 {
		//	line += fmt.Sprintf("\t%d", rec.Mtime)
		//}
		//line += "\n"
		// 格式: start\tend\tmtime\toutput\thash
		line := fmt.Sprintf("%d\t%d\t%d\t%s\t%s\n", rec.StartTime, rec.EndTime, rec.Mtime, rec.Output, rec.CommandHash)
		if _, err := bl.tempFile.WriteString(line); err != nil {
			return err
		}
	}
	return nil
}
