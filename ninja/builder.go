package main

import (
	"path"
)

// Builder 主构建器
type Builder struct {
	state_  *State
	config_ *BuildConfig
	//build_log_        *BuildLog
	//depsLog         *DepsLog
	running_edges_  map[*Edge]int
	disk_interface_ FileSystem
	plan_           *Plan
	status_         Status
	exit_code_      ExitStatus
	explanations_   *Explanations
	scan_           *DependencyScan
	command_runner_ CommandRunner
	lock_file_path_ string
	jobserver_      JobserverClient
	/// Time the build started.
	start_time_millis_ int64
}

func NewBuilder(state *State, config *BuildConfig, buildLog *BuildLog,
	depsLog *DepsLog, start_time_millis int64,
	disk_interface FileSystem, status Status) *Builder {
	b := &Builder{
		state_:  state,
		config_: config,
		//build_log_:           build_log_,
		//depsLog:            depsLog,
		disk_interface_:    disk_interface,
		status_:            status,
		start_time_millis_: start_time_millis,
		lock_file_path_:    ".ninja_lock",
		running_edges_:     make(map[*Edge]int),
	}
	if g_explaining {
		b.explanations_ = NewExplanations()
	}
	b.lock_file_path_ = ".ninja_lock"
	build_dir := b.state_.bindings_.LookupVariable("builddir")
	b.plan_ = NewPlan(b)
	b.scan_ = NewDependencyScan(state, buildLog, depsLog, disk_interface,
		config.depfile_parser_options, b.explanations_)
	if build_dir != "" {
		b.lock_file_path_ = path.Join(build_dir, b.lock_file_path_)
	}
	b.status_.SetExplanations(b.explanations_)
	return b
}

func (b *Builder) Destruct() {
	b.Cleanup()
	b.status_.SetExplanations(nil)
}

func (b *Builder) AddTargetByName(name string, err *string) *Node {
	node := b.state_.LookupNode(name)
	if node == nil {
		*err = "unknown target: '" + name + "'"
		return nil
	}
	if !b.AddTarget(node, err) {
		return nil
	}

	return node
}

func (b *Builder) AddTarget(target *Node, err *string) bool {
	var validationNodes []*Node
	if !b.scan_.RecomputeDirty(target, &validationNodes, err) {
		return false
	}
	if edge := target.in_edge(); edge == nil || !edge.outputs_ready_ {
		if !b.plan_.AddTarget(target, err) {
			return false
		}
	}
	for _, vn := range validationNodes {
		if e := vn.in_edge(); e != nil && !e.outputs_ready_ {
			if !b.plan_.AddTarget(vn, err) {
				return false
			}
		}
	}
	return true
}
func (b *Builder) AlreadyUpToDate() bool {
	return !b.plan_.MoreToDo()
}

func (b *Builder) Build(err *string) ExitStatus {
	if b.AlreadyUpToDate() {
		return ExitSuccess
	}
	b.plan_.PrepareQueue()

	if b.command_runner_ == nil {
		if b.config_.dry_run {
			b.command_runner_ = NewDryRunCommandRunner()
		} else {
			b.command_runner_ = NewRealCommandRunner(b.config_, b.jobserver_)
		}
	}

	b.status_.BuildStarted()
	pendingCommands := 0
	failuresAllowed := b.config_.failures_allowed

	for b.plan_.MoreToDo() {
		// 启动命令
		if failuresAllowed > 0 {
			for b.command_runner_.CanRunMore() > 0 {
				edge := b.plan_.FindWork()
				if edge == nil {
					break
				}
				if edge.GetBindingBool("generator") {
					b.scan_.build_log_.Close()
				}
				if !b.StartEdge(edge, err) {
					b.Cleanup()
					b.status_.BuildFinished()
					return ExitFailure
				}
				if edge.IsPhony() {
					if !b.plan_.EdgeFinished(edge, kEdgeSucceeded, err) {
						b.Cleanup()
						b.status_.BuildFinished()
						return ExitFailure
					}
				} else {
					pendingCommands++
				}
				if pendingCommands == 0 && !b.plan_.MoreToDo() {
					break
				}
			}
		}

		// 等待命令完成
		if pendingCommands > 0 {
			var result CommandResult
			if !b.command_runner_.WaitForCommand(&result) || result.Status == ExitInterrupted {
				b.Cleanup()
				b.status_.BuildFinished()
				*err = "interrupted by user"
				return result.Status
			}
			pendingCommands--
			if !b.FinishCommand(&result, err) {
				b.Cleanup()
				b.status_.BuildFinished()
				return result.Status
			}
			if !result.Success() {
				failuresAllowed--
			}
			continue
		}

		// 无法继续
		b.status_.BuildFinished()
		if failuresAllowed == 0 {
			if b.config_.failures_allowed > 1 {
				*err = "subcommands failed"
			} else {
				*err = "subcommand failed"
			}
		} else {
			*err = "stuck [this is a bug]"
		}
		return b.exit_code_
	}

	b.status_.BuildFinished()
	return ExitSuccess
}

func (b *Builder) StartEdge(edge *Edge, err *string) bool {
	// METRIC_RECORD("StartEdge") - ignored

	if edge.IsPhony() {
		return true
	}

	start_time_millis := GetTimeMillis() - b.start_time_millis_
	b.running_edges_[edge] = int(start_time_millis)

	b.status_.BuildEdgeStarted(edge, start_time_millis)

	var buildStart int64
	if b.config_.dry_run {
		buildStart = 0
	} else {
		buildStart = -1
	}

	// Create directories necessary for outputs and remember the current
	// filesystem mtime to record later
	// XXX: this will block; do we care?
	for _, o := range edge.outputs_ {
		if !b.disk_interface_.MakeDirs(o.path_) {
			return false
		}
		if buildStart == -1 {
			b.disk_interface_.WriteFile(b.lock_file_path_, "", false)
			buildStart = b.disk_interface_.Stat(b.lock_file_path_, err)
			if buildStart == -1 {
				buildStart = 0
			}
		}
	}

	edge.command_start_time_ = buildStart

	// Create depfile directory if needed.
	depfile := edge.GetUnescapedDepfile()
	if depfile != "" && !b.disk_interface_.MakeDirs(depfile) {
		return false
	}

	// Create response log_file_, if needed
	rspfile := edge.GetUnescapedRspfile()
	if rspfile != "" {
		content := edge.GetBinding("rspfile_content")
		if !b.disk_interface_.WriteFile(rspfile, content, true) {
			return false
		}
	}

	// start command computing and run it
	if !b.command_runner_.StartCommand(edge) {
		*err = "command '" + edge.EvaluateCommand(false) + "' failed."
		return false
	}

	return true
}

func (b *Builder) FinishCommand(result *CommandResult, err *string) bool {
	// METRIC_RECORD("FinishCommand") - ignored

	edge := result.Edge

	// First try to extract dependencies from the result, if any.
	// This must happen first as it filters the command output (we want_
	// to filter /showIncludes output, even on compile failure) and
	// extraction itself can fail, which makes the command fail from a
	// build perspective.
	var depsNodes []*Node
	depsType := edge.GetBinding("deps_")
	depsPrefix := edge.GetBinding("msvc_deps_prefix")
	if depsType != "" {
		var extractErr string
		if !b.extractDeps(result, depsType, depsPrefix, &depsNodes, &extractErr) && result.Success() {
			if result.Output != "" {
				result.Output += "\n"
			}
			result.Output += extractErr
			result.Status = ExitFailure
		}
	}

	startTimeMillis, endTimeMillis := int64(0), int64(0)
	it, ok := b.running_edges_[edge]
	if ok {
		startTimeMillis = int64(it)
		delete(b.running_edges_, edge)
	}
	endTimeMillis = GetTimeMillis() - b.start_time_millis_

	b.status_.BuildEdgeFinished(edge, startTimeMillis, endTimeMillis, result.Status, result.Output)

	// The rest of this function only applies to successful commands.
	if !result.Success() {
		return b.plan_.EdgeFinished(edge, kEdgeFailed, err)
	}

	// Restat the edge_ outputs
	var recordMtime int64 = 0
	if !b.config_.dry_run {
		restat := edge.GetBindingBool("restat")
		generator := edge.GetBindingBool("generator")
		nodeCleaned := false
		recordMtime = edge.command_start_time_

		// restat and generator rules must restat the outputs after the build
		// has finished. if recordMtime == 0, then there was an error while
		// attempting to touch/stat the temp log_file_ when the edge_ started and
		// we should fall back to recording the outputs' current mtime in the
		// log.
		if recordMtime == 0 || restat || generator {
			for _, o := range edge.outputs_ {
				newMtime := b.disk_interface_.Stat(o.path_, err)
				if newMtime == -1 {
					return false
				}
				if newMtime > recordMtime {
					recordMtime = newMtime
				}
				if o.mtime_ == newMtime && restat {
					// The rule command did not change the output. Propagate the clean
					// state_ through the build graph.
					// Note that this also applies to nonexistent outputs (mtime == 0).
					if !b.plan_.CleanNode(b.scan_, o, err) {
						return false
					}
					nodeCleaned = true
				}
			}
		}
		if nodeCleaned {
			recordMtime = edge.command_start_time_
		}
	}

	if !b.plan_.EdgeFinished(edge, kEdgeSucceeded, err) {
		return false
	}

	// Delete any left over response log_file_.
	rspfile := edge.GetUnescapedRspfile()
	if rspfile != "" && !g_keep_rsp {
		b.disk_interface_.RemoveFile(rspfile)
	}

	if b.scan_.build_log_ != nil {
		if !b.scan_.build_log_.RecordCommand(edge, int(startTimeMillis), int(endTimeMillis), recordMtime) {
			*err = "Error writing to build log: " // Need to handle errno appropriately
			return false
		}
	}

	if depsType != "" && !b.config_.dry_run {
		if len(edge.outputs_) == 0 {
			panic("should have been rejected by parser")
		}
		for _, o := range edge.outputs_ {
			depsMtime := b.disk_interface_.Stat(o.path_, err)
			if depsMtime == -1 {
				return false
			}
			if !b.scan_.depsLog.RecordDeps1(o, depsMtime, depsNodes) {
				*err = "Error writing to deps_ log: "
				return false
			}
		}
	}
	return true
}

func (b *Builder) extractDeps(result *CommandResult, depsType, depsPrefix string, depsNodes *[]*Node, err *string) bool {
	if depsType == "msvc" {
		parser := NewCLParser()
		var output string
		if !parser.Parse(result.Output, depsPrefix, &output, err) {
			return false
		}
		result.Output = output
		for include := range parser.includes {
			// ~0 is assuming that with MSVC-parsed headers, it's ok to always make
			// all backslashes (as some of the slashes will certainly be backslashes
			// anyway). This could be fixed if necessary with some additional
			// complexity in IncludesNormalize::Relativize.
			*depsNodes = append(*depsNodes, b.state_.GetNode(include, ^uint64(0)))
		}
	} else if depsType == "gcc" {
		depfile := result.Edge.GetUnescapedDepfile()
		if depfile == "" {
			*err = "edge_ with deps_=gcc but no depfile makes no sense"
			return false
		}

		// Read depfile content. Treat a missing depfile as empty.
		var content string
		status := b.disk_interface_.ReadFile(depfile, &content, err)
		if status == StatusNotFound {
			*err = "" // clear error
		} else if status == StatusOtherError {
			return false
		}
		if content == "" {
			return true
		}

		deps := NewDepfileParser(b.config_.depfile_parser_options)
		if !deps.Parse(content, err) {
			return false
		}

		// XXX check depfile matches expected output.
		*depsNodes = make([]*Node, 0, len(deps.Ins))
		for _, input := range deps.Ins {
			// Convert StringPiece to mutable string for canonicalization
			pathStr := input
			slashBits := uint64(0)
			CanonicalizePathString(&pathStr, &slashBits) // assume helper exists
			*depsNodes = append(*depsNodes, b.state_.GetNode(pathStr, slashBits))
		}

		if !g_keep_depfile {
			if errRemove := b.disk_interface_.RemoveFile(depfile); errRemove != 0 {
				*err = "deleting depfile  failed \n"
				return false
			}
		}
	} else {
		panic("unknown deps_ type '" + depsType + "'")
	}

	return true
}

func (b *Builder) LoadDyndeps(node *Node, err *string) bool {
	// 加载 dyndep 信息
	var ddf DyndepFile
	if !b.scan_.LoadDyndeps2(node, &ddf, err) {
		return false
	}
	// 更新构建计划
	if !b.plan_.DyndepsLoaded(b.scan_, node, ddf, err) {

		return false
	}
	return true
}

// Cleanup 清理中断或失败构建产生的临时文件和部分输出。
func (b *Builder) Cleanup() {
	if b.command_runner_ != nil {
		activeEdges := b.command_runner_.GetActiveEdges()
		b.command_runner_.Abort()

		for _, edge := range activeEdges {
			depfile := edge.GetUnescapedDepfile()
			for _, out := range edge.outputs_ {
				// 仅当输出文件被实际修改时才删除。对于 generator 规则，我们可能不希望删除清单文件。
				// 但如果规则使用了 depfile，则始终删除（考虑这种情况：由于 depfile 中提到的头文件修改导致需要重建输出，
				// 但命令在触及输出文件之前被中断）。
				var err string
				newMtime := b.disk_interface_.Stat(out.path_, &err)
				if newMtime == -1 {
					b.status_.Error("%v", err)
				}
				if depfile != "" || out.mtime_ != newMtime {
					b.disk_interface_.RemoveFile(out.path_)
				}
			}
			if depfile != "" {
				b.disk_interface_.RemoveFile(depfile)
			}
		}
	}

	// 删除锁文件
	var err string
	if b.disk_interface_.Stat(b.lock_file_path_, &err) > 0 {
		b.disk_interface_.RemoveFile(b.lock_file_path_)
	}
}

// / Set Jobserver client instance for this builder_.
func (b *Builder) SetJobserverClient(jobserver_client JobserverClient) {
	b.jobserver_ = jobserver_client
}
