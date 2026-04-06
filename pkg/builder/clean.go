package builder

import (
	"fmt"
	"ninja-go/pkg/util"
	"os"
)

// Cleaner 负责清理构建产物。
type Cleaner struct {
	state             *State
	config            *BuildConfig
	dyndepLoader      *DyndepLoader
	removed           map[string]bool
	cleaned           map[*Node]bool
	cleanedFilesCount int
	disk              util.FileSystem
	status            int
}

// NewCleaner 创建 Cleaner 实例。
func NewCleaner(state *State, config *BuildConfig, disk util.FileSystem) *Cleaner {
	return &Cleaner{
		state:        state,
		config:       config,
		dyndepLoader: NewDyndepLoader(state, disk),
		removed:      make(map[string]bool),
		cleaned:      make(map[*Node]bool),
		disk:         disk,
	}
}

// IsVerbose 是否详细输出。
func (c *Cleaner) IsVerbose() bool {
	return c.config.Verbosity != 0 && (c.config.Verbosity == 2 || c.config.DryRun)
}

// RemoveFile 删除文件，返回错误（模拟 C++ 的返回值）。
func (c *Cleaner) RemoveFile(path string) error {
	return c.disk.Remove(path)
}

// FileExists 检查文件是否存在。
func (c *Cleaner) FileExists(path string) bool {
	mtime, err := c.disk.Stat(path)
	if err != nil || mtime.ModTime().UnixMilli() <= 0 {
		return false
	}
	return true
}

// Report 报告已删除的文件。
func (c *Cleaner) Report(path string) {
	c.cleanedFilesCount++
	if c.IsVerbose() {
		fmt.Printf("Remove %s\n", path)
	}
}

// IsAlreadyRemoved 检查文件是否已被标记删除。
func (c *Cleaner) IsAlreadyRemoved(path string) bool {
	return c.removed[path]
}

// Remove 删除文件（如果尚未删除）。
func (c *Cleaner) Remove(path string) {
	if c.IsAlreadyRemoved(path) {
		return
	}
	c.removed[path] = true
	if c.config.DryRun {
		if c.FileExists(path) {
			c.Report(path)
		}
	} else {
		if err := c.RemoveFile(path); err == nil {
			c.Report(path)
		} else {
			c.status = 1
		}
	}
}

// RemoveEdgeFiles 删除边的 depfile 和 rspfile。
func (c *Cleaner) RemoveEdgeFiles(edge *Edge) {
	depfile := edge.GetUnescapedDepfile()
	if depfile != "" {
		c.Remove(depfile)
	}
	rspfile := edge.GetUnescapedRspfile()
	if rspfile != "" {
		c.Remove(rspfile)
	}
}

// PrintHeader 打印清理开始信息。
func (c *Cleaner) PrintHeader() {
	if c.config.Verbosity == 0 {
		return
	}
	fmt.Print("Cleaning...")
	if c.IsVerbose() {
		fmt.Println()
	} else {
		fmt.Print(" ")
	}
}

// PrintFooter 打印清理完成信息。
func (c *Cleaner) PrintFooter() {
	if c.config.Verbosity == 0 {
		return
	}
	fmt.Printf("%d files.\n", c.cleanedFilesCount)
}

// CleanAll 清理所有构建产物（可选清理 generator 规则产生的文件）。
func (c *Cleaner) CleanAll(generator bool) int {
	c.Reset()
	c.PrintHeader()
	c.LoadDyndeps()
	for _, edge := range c.state.Edges {
		if edge.IsPhony() {
			continue
		}
		if !generator && edge.GetBindingBool("generator") {
			continue
		}
		for _, out := range edge.Outputs {
			c.Remove(out.Path)
		}
		c.RemoveEdgeFiles(edge)
	}
	c.PrintFooter()
	return c.status
}

// CleanDead 清理构建日志中不再由 manifest 产生的输出。
func (c *Cleaner) CleanDead(entries map[string]*LogEntry) int {
	c.Reset()
	c.PrintHeader()
	c.LoadDyndeps()
	for output := range entries {
		node := c.state.LookupNode(output)
		if node == nil || (node.InEdge == nil && len(node.OutEdges) == 0) {
			c.Remove(output)
		}
	}
	c.PrintFooter()
	return c.status
}

// DoCleanTarget 递归清理目标及其依赖。
func (c *Cleaner) DoCleanTarget(target *Node) {
	if edge := target.InEdge; edge != nil {
		if !edge.IsPhony() {
			c.Remove(target.Path)
			c.RemoveEdgeFiles(edge)
		}
		for _, in := range edge.Inputs {
			if !c.cleaned[in] {
				c.DoCleanTarget(in)
			}
		}
	}
	c.cleaned[target] = true
}

// CleanTarget 清理单个目标。
func (c *Cleaner) CleanTarget(target *Node) int {
	c.Reset()
	c.PrintHeader()
	c.LoadDyndeps()
	c.DoCleanTarget(target)
	c.PrintFooter()
	return c.status
}

// CleanTargetByName 按名称清理目标。
func (c *Cleaner) CleanTargetByName(targetName string) int {
	node := c.state.LookupNode(targetName)
	if node == nil {
		fmt.Fprintf(os.Stderr, "ninja: unknown target '%s'\n", targetName)
		return 1
	}
	return c.CleanTarget(node)
}

// CleanTargets 清理多个目标。
func (c *Cleaner) CleanTargets(targetNames []string) int {
	c.Reset()
	c.PrintHeader()
	c.LoadDyndeps()
	for _, name := range targetNames {
		norm, _ := util.CanonicalizePath(name)
		target := c.state.LookupNode(norm)
		if target == nil {
			fmt.Fprintf(os.Stderr, "ninja: unknown target '%s'\n", name)
			c.status = 1
			continue
		}
		if c.IsVerbose() {
			fmt.Printf("Target %s\n", norm)
		}
		c.DoCleanTarget(target)
	}
	c.PrintFooter()
	return c.status
}

// DoCleanRule 清理指定规则生成的所有输出。
func (c *Cleaner) DoCleanRule(rule *Rule) {
	for _, edge := range c.state.Edges {
		if edge.Rule.Name == rule.Name {
			for _, out := range edge.Outputs {
				c.Remove(out.Path)
				c.RemoveEdgeFiles(edge)
			}
		}
	}
}

// CleanRule 按规则清理。
func (c *Cleaner) CleanRule(rule *Rule) int {
	c.Reset()
	c.PrintHeader()
	c.LoadDyndeps()
	c.DoCleanRule(rule)
	c.PrintFooter()
	return c.status
}

// CleanRuleByName 按规则名清理。
func (c *Cleaner) CleanRuleByName(ruleName string) int {
	rule := c.state.Bindings.LookupRule(ruleName)
	if rule == nil {
		fmt.Fprintf(os.Stderr, "ninja: unknown rule '%s'\n", ruleName)
		return 1
	}
	return c.CleanRule(rule)
}

// CleanRules 清理多个规则。
func (c *Cleaner) CleanRules(ruleNames []string) int {
	c.Reset()
	c.PrintHeader()
	c.LoadDyndeps()
	for _, name := range ruleNames {
		rule := c.state.Bindings.LookupRule(name)
		if rule == nil {
			fmt.Fprintf(os.Stderr, "ninja: unknown rule '%s'\n", name)
			c.status = 1
			continue
		}
		if c.IsVerbose() {
			fmt.Printf("Rule %s\n", name)
		}
		c.DoCleanRule(rule)
	}
	c.PrintFooter()
	return c.status
}

// Reset 重置内部状态。
func (c *Cleaner) Reset() {
	c.status = 0
	c.cleanedFilesCount = 0
	c.removed = make(map[string]bool)
	c.cleaned = make(map[*Node]bool)
}

// LoadDyndeps 加载所有挂起的 dyndep 文件（忽略错误）。
func (c *Cleaner) LoadDyndeps() {
	for _, edge := range c.state.Edges {
		if dyndepNode := edge.DyndepFile; dyndepNode != nil && dyndepNode.DyndepPending {
			// 忽略错误，尽可能清理
			_ = c.dyndepLoader.LoadDyndeps(dyndepNode)
		}
	}
}
