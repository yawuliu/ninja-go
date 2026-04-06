package builder

import (
	"fmt"
	"os"
)

// Status 状态显示接口（可简化）

// Status 接口
type Status interface {
	EdgeAddedToPlan(edge *Edge)
	EdgeRemovedFromPlan(edge *Edge)
	BuildEdgeStarted(edge *Edge, startTimeMillis int64)
	BuildEdgeFinished(edge *Edge, startTimeMillis, endTimeMillis int64, exitCode ExitStatus, output string)
	BuildStarted()
	BuildFinished()
	SetExplanations(expl *Explanations)
	Info(msg string, args ...interface{})
	Warning(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// ConsoleStatus 默认的控制台输出实现。
type ConsoleStatus struct {
	config       BuildConfig
	explanations *Explanations
}

// NewStatus 根据 BuildConfig 创建 Status 实例。
func NewStatus(config BuildConfig) Status {
	return &ConsoleStatus{config: config}
}

func (s *ConsoleStatus) EdgeAddedToPlan(edge *Edge) {
	if s.config.Verbosity >= VerbosityNormal {
		// 可显示边缘加入计划的信息，通常不输出
	}
}

func (s *ConsoleStatus) EdgeRemovedFromPlan(edge *Edge) {
	// 通常不需要输出
}

func (s *ConsoleStatus) BuildEdgeStarted(edge *Edge, startTimeMillis int64) {
	if s.config.Verbosity >= VerbosityNormal {
		// 输出 "[N/N] 命令..."，这里简化
		cmd := edge.EvaluateCommand(false)
		// 截断长命令？可选
		fmt.Printf("[%d/%d] %s\n", 0, 0, cmd) // 实际需要知道总数，这里简化
	}
}

func (s *ConsoleStatus) BuildEdgeFinished(edge *Edge, startTimeMillis, endTimeMillis int64, exitCode ExitStatus, output string) {
	if exitCode != ExitSuccess && output != "" {
		// 输出错误输出
		fmt.Print(output)
	}
}

func (s *ConsoleStatus) BuildStarted() {
	// 可选：输出开始信息
}

func (s *ConsoleStatus) BuildFinished() {
	// 可选：输出结束信息
}

func (s *ConsoleStatus) SetExplanations(expl *Explanations) {
	s.explanations = expl
}

func (s *ConsoleStatus) Info(msg string, args ...interface{}) {
	if s.config.Verbosity >= VerbosityNormal {
		fmt.Printf("ninja: "+msg+"\n", args...)
	}
}

func (s *ConsoleStatus) Warning(msg string, args ...interface{}) {
	// 警告总是输出
	fmt.Fprintf(Stderr, "ninja: warning: "+msg+"\n", args...)
}

func (s *ConsoleStatus) Error(msg string, args ...interface{}) {
	fmt.Fprintf(Stderr, "ninja: error: "+msg+"\n", args...)
}

// 辅助变量，用于测试时可重定向
var Stderr = ConsoleStderr()

type consoleStderr struct{}

func (c consoleStderr) Write(p []byte) (n int, err error) {
	return os.Stderr.Write(p)
}

func ConsoleStderr() *consoleStderr { return &consoleStderr{} }

// Verbosity 等级定义
const (
	VerbosityQuiet = iota
	VerbosityNoStatusUpdate
	VerbosityNormal
	VerbosityVerbose
)
