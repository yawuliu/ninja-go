package builder

import (
	"bufio"
	"errors"
	"fmt"
	"hash/fnv"
	"ninja-go/pkg/util"
	"os"
	"strconv"
	"strings"
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
	CommandHash string
	StartTime   int64
	EndTime     int64
	Mtime       int64
}

// BuildLog 管理 .ninja_log 文件
type BuildLog struct {
	mu                sync.RWMutex
	filePath          string
	file              *os.File
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
	if bl.file != nil {
		err := bl.file.Close()
		bl.file = nil
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
	if bl.file == nil {
		panic(errors.New("build log not opened"))
	}
	bl.filePath = path
	return true
}

// openForWriteIfNeeded 在首次写入时打开文件并写入签名
func (bl *BuildLog) openForWriteIfNeeded() bool {
	if bl.file != nil || bl.filePath == "" {
		return true
	}
	f, err := os.OpenFile(bl.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false
	}
	bl.file = f
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
func HashCommand(command string) string {
	h := fnv.New64a()
	h.Write([]byte(command))
	return strconv.FormatUint(h.Sum64(), 10)
}

// RecordCommand 记录一条边的构建信息
func (bl *BuildLog) RecordCommand(edge *Edge, start, end int64, mtime int64) bool {
	command := edge.EvaluateCommand(true)
	commandHash := HashCommand(command) // 使用 SHA256 或 rapidhash

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

		if !bl.openForWriteIfNeeded() {
			return false
		}
		if bl.file != nil {
			if !bl.writeEntry(bl.file, entry) {
				return false
			}
			if err := bl.file.Sync(); err != nil {
				return false
			}
		}
	}
	return true
}

// writeEntry 将单条记录写入文件
func (bl *BuildLog) writeEntry(f *os.File, entry *LogEntry) bool {
	_, err := fmt.Fprintf(f, "%d\t%d\t%d\t%s\t%s\n",
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

// Load 从磁盘加载日志
func (bl *BuildLog) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	bl.mu.Lock()
	defer bl.mu.Unlock()
	bl.entries = make(map[string]*LogEntry)

	scanner := bufio.NewScanner(f)
	// 增大缓冲区以处理长行
	const maxCapacity = 512 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	logVersion := 0
	uniqueCount := 0
	totalCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if logVersion == 0 {
			// 解析签名行
			if n, _ := fmt.Sscanf(line, kBuildLogFileSignature, &logVersion); n == 1 {
				if logVersion < kOldestSupportedVersion || logVersion > kBuildLogCurrentVersion {
					// 版本不兼容，删除文件并返回成功（视为空日志）
					f.Close()
					os.Remove(path)
					return nil
				}
				continue
			}
		}
		// 解析数据行: start end mtime output hash
		parts := strings.Split(line, "\t")
		if len(parts) < 5 {
			continue
		}
		start, _ := strconv.ParseInt(parts[0], 10, 64)
		end, _ := strconv.ParseInt(parts[1], 10, 64)
		mtime, _ := strconv.ParseInt(parts[2], 10, 64)
		output := parts[3]
		hash := parts[4]

		if _, ok := bl.entries[output]; !ok {
			uniqueCount++
		}
		bl.entries[output] = &LogEntry{
			Output:      output,
			CommandHash: hash,
			StartTime:   start,
			EndTime:     end,
			Mtime:       mtime,
		}
		totalCount++
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// 决定是否需要 recompact
	const kMinCompactionEntryCount = 100
	const kCompactionRatio = 3
	if logVersion < kBuildLogCurrentVersion {
		bl.needsRecompaction = true
	} else if totalCount > kMinCompactionEntryCount &&
		totalCount > uniqueCount*kCompactionRatio {
		bl.needsRecompaction = true
	}
	return nil
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
