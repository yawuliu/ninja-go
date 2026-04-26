package main

import (
	"container/list"
)

// DryRunCommandRunner 是一个不实际运行命令的 CommandRunner，用于干运行模式。
type DryRunCommandRunner struct {
	finished *list.List // 存储已“完成”的 Edge
}

// Verify that *UserCacher implements Cacher
var _ CommandRunner = (*DryRunCommandRunner)(nil)

func (r *DryRunCommandRunner) GetActiveEdges() []*Edge {
	return []*Edge{}
}

func (r *DryRunCommandRunner) Abort() {}

// NewDryRunCommandRunner 创建干运行命令执行器
func NewDryRunCommandRunner() *DryRunCommandRunner {
	return &DryRunCommandRunner{
		finished: list.New(),
	}
}

// CanRunMore 干运行模式下总是可以运行更多命令（返回较大值）
func (r *DryRunCommandRunner) CanRunMore() int {
	// 模拟无限制容量
	return 1 << 30
}

// StartCommand 模拟启动命令：将 edge_ 放入完成队列
func (r *DryRunCommandRunner) StartCommand(edge *Edge) bool {
	r.finished.PushBack(edge)
	return true
}

// WaitForCommand 等待一个命令完成：从队列中取出一个 edge_，并返回成功结果
func (r *DryRunCommandRunner) WaitForCommand(result *CommandResult) bool {
	if r.finished.Len() == 0 {
		return false
	}
	result.Status = ExitSuccess
	front := r.finished.Front()
	result.Edge = front.Value.(*Edge)
	r.finished.Remove(front)
	return true
}
