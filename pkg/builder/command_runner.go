package builder

// CommandRunner 接口
type CommandRunner interface {
	CanRunMore() int
	StartCommand(edge *Edge) error
	WaitForCommand() (*CommandResult, error)
	GetActiveEdges() []*Edge
	Abort()
}

type CommandResult struct {
	Edge      *Edge
	Status    ExitStatus
	Output    string
	StartTime int64
}

func (r *CommandResult) Success() bool { return r.Status == ExitSuccess }
