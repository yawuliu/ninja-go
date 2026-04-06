package builder

type VisitMark int8

const (
	VisitNone VisitMark = iota
	VisitInStack
	VisitDone
)

type Edge struct {
	Rule                 *Rule
	Pool                 *Pool
	Inputs               []*Node
	Outputs              []*Node
	Validations          []*Node
	DyndepFile           *Node
	Env                  *BindingEnv
	ID                   uint64
	CriticalPathWeight   int64
	OutputsReady         bool
	DepsLoaded           bool
	DepsMissing          bool
	GeneratedByDepLoader bool
	CommandStartTime     int64
	ImplicitDeps         int
	OrderOnlyDeps        int
	ImplicitOuts         int
	Mark                 VisitMark // 0 none, 1 in stack, 2 done
	/// A Jobserver slot instance. Invalid by default.
	jobSlot JobserverSlot
}

func (e *Edge) AllInputsReady() bool {
	for _, in := range e.Inputs {
		if in.InEdge != nil && !in.InEdge.OutputsReady {
			return false
		}
	}
	return true
}

func (e *Edge) EvaluateCommand(incl_rsp_file bool) string {
	command := e.GetBinding("command")
	if incl_rsp_file {
		rspfile_content := e.GetBinding("rspfile_content")
		if rspfile_content != "" {
			command += ";rspfile=" + rspfile_content
		}
	}
	return command
}

func (e *Edge) GetBinding(key string) string {
	env := &EdgeEnv{edge: e, escapeInOut: true}
	return env.LookupVariable(key)
}

func (e *Edge) GetBindingBool(key string) bool {
	return e.GetBinding(key) != ""
}

func (e *Edge) GetUnescapedDepfile() string {
	env := &EdgeEnv{edge: e, escapeInOut: false}
	return env.LookupVariable("depfile")
}

func (e *Edge) GetUnescapedDyndep() string {
	env := &EdgeEnv{edge: e, escapeInOut: false}
	return env.LookupVariable("dyndep")
}

func (e *Edge) GetUnescapedRspfile() string {
	env := &EdgeEnv{edge: e, escapeInOut: false}
	return env.LookupVariable("rspfile")
}

func (e *Edge) IsImplicit(idx int) bool {
	n := len(e.Inputs)
	return idx >= n-e.OrderOnlyDeps-e.ImplicitDeps && !e.IsOrderOnly(idx)
}

func (e *Edge) IsOrderOnly(idx int) bool {
	return idx >= len(e.Inputs)-e.OrderOnlyDeps
}

func (e *Edge) IsImplicitOut(idx int) bool {
	return idx >= len(e.Outputs)-e.ImplicitOuts
}

func (e *Edge) IsPhony() bool {
	return e.Rule != nil && e.Rule.Name == "phony"
}

func (e *Edge) UseConsole() bool {
	return e.Pool != nil && e.Pool.Name == "console"
}
func (e *Edge) Weight() int { return 1 }

func (e *Edge) MaybePhonyCycleDiagnostic() bool {
	return e.IsPhony() && len(e.Outputs) == 1 && e.ImplicitOuts == 0 && e.ImplicitDeps == 0
}
