package builder

import "ninja-go/pkg/graph"

// CommandRunner 接口
type CommandRunner interface {
	CanRunMore() int
	StartCommand(edge *graph.Edge) error
	WaitForCommand() (*CommandResult, error)
	GetActiveEdges() []*graph.Edge
	Abort()
}

type CommandResult struct {
	Edge      *graph.Edge
	Status    ExitStatus
	Output    string
	StartTime int64
}

func (r *CommandResult) Success() bool { return r.Status == ExitSuccess }
