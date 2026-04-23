package main

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/fnv"
	"io"
	"ninja-go/ninja/util"
	"os"
	"strconv"
	"sync"
)

const (
	kBuildLogFileSignature  = "# ninja log v%d\n"
	kOldestSupportedVersion = 7
	kBuildLogCurrentVersion = 7
)

// BuildLogUser 接口，用于判断某个输出是否已死亡（用于 recompact）
type BuildLogUser interface {
	IsPathDead(path string) bool
}

// LogEntry 表示一条构建记录
type LogEntry struct {
	Output      string
	CommandHash uint64
	StartTime   int
	EndTime     int
	Mtime       int64
}

// BuildLog 管理 .ninja_log 文件
type BuildLog struct {
	mu                sync.RWMutex
	filePath          string
	log_file_         *os.File
	entries           map[string]*LogEntry
	needsRecompaction bool
}

func NewBuildLog(path string) *BuildLog {
	return &BuildLog{
		filePath: path,
		entries:  make(map[string]*LogEntry),
	}
}

// Close 关闭日志文件
func (bl *BuildLog) Close() error {
	bl.openForWriteIfNeeded() // 确保文件已创建（即使没有记录）
	if bl.log_file_ != nil {
		err := bl.log_file_.Close()
		bl.log_file_ = nil
		return err
	}
	return nil
}

// OpenForWrite 准备写入日志，如果需要则先执行 recompact
func (bl *BuildLog) OpenForWrite(path string, user BuildLogUser, err *string) bool {
	if bl.needsRecompaction {
		if !bl.Recompact(path, user, err) {
			return false
		}
	}
	if bl.log_file_ != nil {
		panic("log_file_ should be nil")
	}
	bl.filePath = path
	return true
}

// openForWriteIfNeeded 在首次写入时打开文件并写入签名
func (bl *BuildLog) openForWriteIfNeeded() bool {
	if bl.log_file_ != nil || bl.filePath == "" {
		return true
	}
	f, err := os.OpenFile(bl.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false
	}
	bl.log_file_ = f
	// 检查文件是否为空
	info, err := f.Stat()
	if err != nil {
		return false
	}
	if info.Size() == 0 {
		if _, err := fmt.Fprintf(f, kBuildLogFileSignature, kBuildLogCurrentVersion); err != nil {
			return false
		}
	}
	return true
}

// HashCommand 计算命令字符串的快速哈希值（与 C++ 的 rapidhash 类似）
func HashCommand(command string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(command))
	return h.Sum64()
}

// RecordCommand 记录一条边的构建信息
func (bl *BuildLog) RecordCommand(edge *Edge, start, end int, mtime int64) bool {
	command := edge.EvaluateCommand(true)
	commandHash := HashCommand(command) // 使用 SHA256 或 rapidhash

	for _, out := range edge.outputs_ {
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

		if !bl.openForWriteIfNeeded() {
			return false
		}
		if bl.log_file_ != nil {
			if !bl.writeEntry(bl.log_file_, entry) {
				return false
			}
			if err := bl.log_file_.Sync(); err != nil {
				return false
			}
		}
	}
	return true
}

// writeEntry 将单条记录写入文件
func (bl *BuildLog) writeEntry(f *os.File, entry *LogEntry) bool {
	_, err := fmt.Fprintf(f, "%d\t%d\t%d\t%s\t%d\n",
		entry.StartTime, entry.EndTime, entry.Mtime,
		entry.Output, entry.CommandHash)
	if err != nil {
		return false
	}
	return true
}

// LookupByOutput 根据输出路径查找记录
func (bl *BuildLog) LookupByOutput(path string) *LogEntry {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	return bl.entries[path]
}

type LoadStatus int

const (
	LOAD_ERROR LoadStatus = iota
	LOAD_SUCCESS
	LOAD_NOT_FOUND
)

func (bl *BuildLog) Load(path string, err *string) LoadStatus {
	// METRIC_RECORD(".ninja_log load") - ignored

	file, errOpen := os.Open(path)
	if errOpen != nil {
		if os.IsNotExist(errOpen) {
			return LOAD_NOT_FOUND
		}
		*err = errOpen.Error()
		return LOAD_ERROR
	}
	defer file.Close()

	logVersion := 0
	uniqueEntryCount := 0
	totalEntryCount := 0

	reader := bufio.NewReader(file)
	// lineNum := 0
	for {
		line, errRead := reader.ReadBytes('\n')
		if errRead != nil && errRead != io.EOF {
			*err = errRead.Error()
			return LOAD_ERROR
		}
		if len(line) == 0 && errRead == io.EOF {
			break
		}
		// Remove trailing newline
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		if len(line) == 0 {
			continue
		}

		if logVersion == 0 {
			// First line: version signature
			if n, _ := fmt.Sscanf(string(line), kBuildLogFileSignature, &logVersion); n != 1 {
				// Not a valid version line? Treat as old version?
				// Original code uses sscanf which may fail; we'll handle similarly.
				// For simplicity, assume the line matches the signature.
			}
			if logVersion < kOldestSupportedVersion {
				*err = "build log version is too old; starting over"
				file.Close()
				os.Remove(path) // platformAwareUnlink
				return LOAD_NOT_FOUND
			} else if logVersion > kBuildLogCurrentVersion {
				*err = "build log version is too new; starting over"
				file.Close()
				os.Remove(path)
				return LOAD_NOT_FOUND
			}
			continue
		}

		// Parse a log line: start_time\tend_time\tmtime\toutput\tcommand_hash
		parts := bytes.Split(line, []byte{'\t'})
		if len(parts) < 5 {
			continue // skip malformed lines
		}
		startTime, _ := strconv.Atoi(string(parts[0]))
		endTime, _ := strconv.Atoi(string(parts[1]))
		mtime, _ := strconv.ParseInt(string(parts[2]), 10, 64)
		output := string(parts[3])
		commandHash, _ := strconv.ParseUint(string(parts[4]), 10, 64)

		entry, ok := bl.entries[output]
		if !ok {
			entry = &LogEntry{Output: output}
			bl.entries[output] = entry
			uniqueEntryCount++
		}
		totalEntryCount++

		entry.StartTime = startTime
		entry.EndTime = endTime
		entry.Mtime = mtime
		entry.CommandHash = commandHash
	}

	// Decide whether to recompact
	const kMinCompactionEntryCount = 100
	const kCompactionRatio = 3
	if logVersion < kCurrentVersion {
		bl.needsRecompaction = true
	} else if totalEntryCount > kMinCompactionEntryCount &&
		totalEntryCount > uniqueEntryCount*kCompactionRatio {
		bl.needsRecompaction = true
	}

	return LOAD_SUCCESS
}

// Recompact 重新压实日志，删除死亡记录
func (bl *BuildLog) Recompact(path string, user BuildLogUser, err *string) bool {
	bl.Close()
	temp_path := path + ".recompact"
	f, create_err := os.Create(temp_path)
	if create_err != nil {
		*err = create_err.Error()
		return false
	}
	defer f.Close()

	if _, dump_err := fmt.Fprintf(f, kBuildLogFileSignature, kBuildLogCurrentVersion); dump_err != nil {
		*err = dump_err.Error()
		return false
	}

	bl.mu.RLock()
	defer bl.mu.RUnlock()

	for output, entry := range bl.entries {
		if user.IsPathDead(output) {
			continue
		}
		if !bl.writeEntry(f, entry) {
			*err = fmt.Sprintf("")
			return false
		}
	}
	// 删除死亡记录
	for output := range bl.entries {
		if user.IsPathDead(output) {
			delete(bl.entries, output)
		}
	}

	return util.ReplaceContent(path, temp_path, err)
}

// Restat 更新日志中某些输出的 mtime（用于 restat 规则）
func (bl *BuildLog) Restat(path string, disk util.FileSystem, outputs []string, err *string) bool {
	bl.Close()
	tempPath := path + ".restat"
	f, create_err := os.Create(tempPath)
	if create_err != nil {
		*err = create_err.Error()
		return false
	}
	defer f.Close()

	if _, dump_err := fmt.Fprintf(f, kBuildLogFileSignature, kBuildLogCurrentVersion); dump_err != nil {
		*err = dump_err.Error()
		return false
	}

	bl.mu.Lock()
	defer bl.mu.Unlock()

	skipMap := make(map[string]bool)
	for _, out := range outputs {
		skipMap[out] = true
	}

	for output, entry := range bl.entries {
		// 如果输出在 outputs 列表中，则更新其 mtime
		if skipMap[output] {
			mtime := disk.Stat(output, err)
			if mtime == -1 {
				return false
			}
			entry.Mtime = mtime
		}
		if !bl.writeEntry(f, entry) {
			*err = fmt.Sprintf("failed to write entry for output %s", output)
			return false
		}
	}

	return util.ReplaceContent(tempPath, path, err)
}

func (bl *BuildLog) Entries() map[string]*LogEntry { return bl.entries }
