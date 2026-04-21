package builder

import (
	"ninja-go/pkg/util"
	"strings"
)

// CLParser 解析 MSVC 编译器的 /showIncludes 输出
type CLParser struct {
	includes map[string]bool // 存储规范化后的包含文件路径
}

// NewCLParser 创建 CLParser 实例
func NewCLParser() *CLParser {
	return &CLParser{
		includes: make(map[string]bool),
	}
}

// FilterShowIncludes 从一行输出中提取包含文件路径（如果该行是 /showIncludes 输出）
func FilterShowIncludes(line, depsPrefix string) string {
	const kDepsPrefixEnglish = "Note: including log_file_: "
	prefix := depsPrefix
	if prefix == "" {
		prefix = kDepsPrefixEnglish
	}
	if len(line) > len(prefix) && line[:len(prefix)] == prefix {
		// 去掉前缀，并跳过后续空格
		rest := line[len(prefix):]
		rest = strings.TrimLeft(rest, " ")
		return rest
	}
	return ""
}

// IsSystemInclude 判断路径是否为系统头文件（启发式）
func IsSystemInclude(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "program files") ||
		strings.Contains(lower, "microsoft visual studio")
}

// FilterInputFilename 判断文件名是否为源文件（扩展名匹配）
func FilterInputFilename(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".c") ||
		strings.HasSuffix(lower, ".cc") ||
		strings.HasSuffix(lower, ".cxx") ||
		strings.HasSuffix(lower, ".cpp") ||
		strings.HasSuffix(lower, ".c++")
}

// Parse 解析编译器输出，过滤掉 /showIncludes 和输入文件名行，收集包含文件路径
// 参数：
//
//	output: 原始输出字符串
//	depsPrefix: 自定义的 /showIncludes 前缀（通常为空）
//	currentDir: 当前工作目录（用于路径规范化，Windows 需要）
//
// 返回：
//
//	filteredOutput: 过滤后的输出（保留非相关行）
//	includes: 规范化后的包含文件路径列表
//	error: 错误信息
func (p *CLParser) Parse(output, depsPrefix string, filtered_output, err *string) bool {
	p.includes = make(map[string]bool)
	var filtered strings.Builder
	lines := strings.Split(output, "\n")
	seenShowIncludes := false

	for _, line := range lines {
		// 去除行尾回车（但保留换行符在输出中）
		rawLine := strings.TrimRight(line, "\r")
		include := FilterShowIncludes(rawLine, depsPrefix)
		if include != "" {
			seenShowIncludes = true
			// 规范化路径
			normalized := include
			var slash_bits uint64
			util.CanonicalizePathString(&normalized, &slash_bits)
			if !IsSystemInclude(normalized) {
				p.includes[normalized] = true
			}
		} else if !seenShowIncludes && FilterInputFilename(rawLine) {
			// 丢弃输入文件名行（在未看到 /showIncludes 之前）
			continue
		} else {
			// 保留其他行
			filtered.WriteString(line)
			filtered.WriteByte('\n')
		}
	}
	// 收集 includes
	includeList := make([]string, 0, len(p.includes))
	for inc := range p.includes {
		includeList = append(includeList, inc)
	}
	return true
}
