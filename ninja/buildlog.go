package main

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"strconv"
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
	output       string
	command_hash uint64
	start_time   int
	end_time     int
	mtime        int64
}

// BuildLog 管理 .ninja_log 文件
type BuildLog struct {
	log_file_path_      string
	log_file_           *os.File
	entries_            map[string]*LogEntry
	needs_recompaction_ bool
}

func NewBuildLog(path string) *BuildLog {
	return &BuildLog{
		log_file_path_:      path,
		entries_:            make(map[string]*LogEntry),
		needs_recompaction_: false,
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
	if bl.needs_recompaction_ {
		if !bl.Recompact(path, user, err) {
			return false
		}
	}
	if bl.log_file_ != nil {
		panic("log_file_ should be nil")
	}
	bl.log_file_path_ = path
	return true
}

// openForWriteIfNeeded 在首次写入时打开文件并写入签名
func (bl *BuildLog) openForWriteIfNeeded() bool {
	if bl.log_file_ != nil || bl.log_file_path_ == "" {
		return true
	}
	f, err := os.OpenFile(bl.log_file_path_, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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
		path := out.path_
		entry, ok := bl.entries_[path]
		if !ok {
			entry = &LogEntry{output: path}
			bl.entries_[path] = entry
		}
		entry.command_hash = commandHash
		entry.start_time = start
		entry.end_time = end
		entry.mtime = mtime

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
		entry.start_time, entry.end_time, entry.mtime,
		entry.output, entry.command_hash)
	if err != nil {
		return false
	}
	return true
}

// LookupByOutput 根据输出路径查找记录
func (bl *BuildLog) LookupByOutput(path string) *LogEntry {
	if _, ok := bl.entries_[path]; ok {
		return bl.entries_[path]
	}
	return nil
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

		entry, ok := bl.entries_[output]
		if !ok {
			entry = &LogEntry{output: output}
			bl.entries_[output] = entry
			uniqueEntryCount++
		}
		totalEntryCount++

		entry.start_time = startTime
		entry.end_time = endTime
		entry.mtime = mtime
		entry.command_hash = commandHash
	}

	// Decide whether to recompact
	const kMinCompactionEntryCount = 100
	const kCompactionRatio = 3
	if logVersion < kBuildLogCurrentVersion {
		bl.needs_recompaction_ = true
	} else if totalEntryCount > kMinCompactionEntryCount &&
		totalEntryCount > uniqueEntryCount*kCompactionRatio {
		bl.needs_recompaction_ = true
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

	if _, dump_err := fmt.Fprintf(f, kBuildLogFileSignature, kBuildLogCurrentVersion); dump_err != nil {
		f.Close()
		*err = dump_err.Error()
		return false
	}

	for output, entry := range bl.entries_ {
		if user.IsPathDead(output) {
			continue
		}
		if !bl.writeEntry(f, entry) {
			f.Close()
			*err = ""
			return false
		}
	}
	// 删除死亡记录
	for output := range bl.entries_ {
		if user.IsPathDead(output) {
			delete(bl.entries_, output)
		}
	}

	f.Close()
	return ReplaceContent(path, temp_path, err)
}

// Restat 更新日志中某些输出的 mtime（用于 restat 规则）
func (bl *BuildLog) Restat(path string, disk FileSystem, outputs []string, err *string) bool {
	bl.Close()
	tempPath := path + ".restat"
	f, create_err := os.Create(tempPath)
	if create_err != nil {
		*err = create_err.Error()
		return false
	}

	if _, dump_err := fmt.Fprintf(f, kBuildLogFileSignature, kBuildLogCurrentVersion); dump_err != nil {
		f.Close()
		*err = dump_err.Error()
		return false
	}

	skipMap := make(map[string]bool)
	hasFilter := len(outputs) > 0
	for _, out := range outputs {
		skipMap[out] = true
	}

	for output, entry := range bl.entries_ {
		// 如果指定了 outputs 过滤列表，只更新列表中的条目
		// 如果没有指定过滤条件，更新所有条目
		skip := hasFilter && !skipMap[output]
		if !skip {
			mtime := disk.Stat(output, err)
			if mtime == -1 {
				f.Close()
				return false
			}
			entry.mtime = mtime
		}
		if !bl.writeEntry(f, entry) {
			f.Close()
			*err = fmt.Sprintf("failed to write entry for output %s", output)
			return false
		}
	}

	f.Close()
	return ReplaceContent(tempPath, path, err)
}

func (bl *BuildLog) Entries() map[string]*LogEntry { return bl.entries_ }
