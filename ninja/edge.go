package main

type VisitMark int8

const (
	VisitNone VisitMark = iota
	VisitInStack
	VisitDone
)

type Edge struct {
	rule_                    *Rule
	pool_                    *Pool
	inputs_                  []*Node
	outputs_                 []*Node
	validations_             []*Node
	dyndep_                  *Node
	env_                     *BindingEnv
	id_                      uint64
	critical_path_weight_    int64
	outputs_ready_           bool
	deps_loaded_             bool
	deps_missing_            bool
	generated_by_dep_loader_ bool
	command_start_time_      int64
	implicit_deps_           int
	order_only_deps_         int
	implicit_outs_           int
	mark_                    VisitMark // 0 none, 1 in stack, 2 done
	/// A Jobserver slot instance. Invalid by default.
	job_slot_ JobserverSlot
	// Historical info: how long did this edge_ take last time,
	// as per .ninja_log, if known? defaults_ to -1 if unknown.
	prev_elapsed_time_millis int64 // -1;
}

func (e *Edge) AllInputsReady() bool {
	for _, in := range e.inputs_ {
		if in.in_edge() != nil && !in.in_edge().outputs_ready_ {
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
	env := NewEdgeEnv(e, kShellEscape)
	return env.LookupVariable(key)
}

func (e *Edge) GetBindingBool(key string) bool {
	return e.GetBinding(key) != ""
}

func (e *Edge) GetUnescapedDepfile() string {
	env := NewEdgeEnv(e, kDoNotEscape)
	return env.LookupVariable("depfile")
}

func (e *Edge) GetUnescapedDyndep() string {
	env := NewEdgeEnv(e, kDoNotEscape)
	return env.LookupVariable("dyndep")
}

func (e *Edge) GetUnescapedRspfile() string {
	env := NewEdgeEnv(e, kDoNotEscape)
	return env.LookupVariable("rspfile")
}

func (e *Edge) IsImplicit(idx int) bool {
	n := len(e.inputs_)
	return idx >= n-e.order_only_deps_-e.implicit_deps_ && !e.is_order_only(idx)
}

func (e *Edge) is_order_only(index int) bool {
	return index >= len(e.inputs_)-e.order_only_deps_
}

func (e *Edge) IsImplicitOut(idx int) bool {
	return idx >= len(e.outputs_)-e.implicit_outs_
}

func (e *Edge) IsPhony() bool {
	return e.rule_ != nil && e.rule_.Name == "phony"
}

func (e *Edge) use_console() bool {
	return e.pool_ == kConsolePool
}
func (e *Edge) Weight() int { return 1 }

func (e *Edge) MaybePhonyCycleDiagnostic() bool {
	return e.IsPhony() && len(e.outputs_) == 1 && e.implicit_outs_ == 0 && e.implicit_deps_ == 0
}
