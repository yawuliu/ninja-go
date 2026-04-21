package builder

import (
	"ninja-go/pkg/util"
	"path"
)

// Builder 主构建器
type Builder struct {
	state          *State
	config         *BuildConfig
	buildLog       *BuildLog
	depsLog        *DepsLog
	running_edges_ map[*Edge]int
	disk           util.FileSystem
	plan           *Plan
	status         Status
	exitCode       ExitStatus
	explanations   *Explanations
	scan           *DependencyScan
	commandRunner  CommandRunner
	lockFilePath   string
	jobserver_     JobserverClient
	/// Time the build started.
	start_time_millis_ int64
}

func NewBuilder(state *State, config *BuildConfig, buildLog *BuildLog,
	depsLog *DepsLog, start_time_millis int64,
	disk_interface util.FileSystem, status Status) *Builder {
	b := &Builder{
		state:              state,
		config:             config,
		buildLog:           buildLog,
		depsLog:            depsLog,
		disk:               disk_interface,
		status:             status,
		start_time_millis_: start_time_millis,
		lockFilePath:       ".ninja_lock",
	}
	b.lockFilePath = ".ninja_lock"
	build_dir := state.Bindings.LookupVariable("builddir")
	if build_dir != "" {
		b.lockFilePath = path.Join(build_dir, b.lockFilePath)
	}
	b.plan = NewPlan(b)
	b.scan = NewDependencyScan(state, buildLog, depsLog, disk_interface,
		config.DepfileParserOptions, b.explanations)
	return b
}

func (b *Builder) Destruct() {
	b.Cleanup()
	b.status.SetExplanations(nil)
}

func (b *Builder) AddTargetByName(name string, err *string) *Node {
	node := b.state.LookupNode(name)
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
	if !b.scan.RecomputeDirty(target, &validationNodes, err) {
		return false
	}
	if edge := target.InEdge; edge == nil || !edge.OutputsReady {
		if !b.plan.AddTarget(target, err) {
			return false
		}
	}
	for _, vn := range validationNodes {
		if e := vn.InEdge; e != nil && !e.OutputsReady {
			if !b.plan.AddTarget(vn, err) {
				return false
			}
		}
	}
	return true
}
func (b *Builder) AlreadyUpToDate() bool {
	return !b.plan.MoreToDo()
}

func (b *Builder) Build(err *string) ExitStatus {
	if b.AlreadyUpToDate() {
		return ExitSuccess
	}
	b.plan.PrepareQueue()

	if b.commandRunner == nil {
		if b.config.DryRun {
			b.commandRunner = NewDryRunCommandRunner()
		} else {
			b.commandRunner = NewRealCommandRunner(b.config, b.jobserver_)
		}
	}

	b.status.BuildStarted()
	pendingCommands := 0
	failuresAllowed := b.config.FailuresAllowed

	for b.plan.MoreToDo() {
		// 启动命令
		if failuresAllowed > 0 {
			for b.commandRunner.CanRunMore() > 0 {
				edge := b.plan.FindWork()
				if edge == nil {
					break
				}
				if edge.GetBindingBool("generator") {
					b.buildLog.Close()
				}
				if !b.StartEdge(edge, err) {
					b.Cleanup()
					b.status.BuildFinished()
					return ExitFailure
				}
				if edge.IsPhony() {
					if !b.plan.EdgeFinished(edge, EdgeSucceeded, err) {
						b.Cleanup()
						b.status.BuildFinished()
						return ExitFailure
					}
				} else {
					pendingCommands++
				}
				if pendingCommands == 0 && !b.plan.MoreToDo() {
					break
				}
			}
		}

		// 等待命令完成
		if pendingCommands > 0 {
			var result CommandResult
			if !b.commandRunner.WaitForCommand(&result) || result.Status == ExitInterrupted {
				b.Cleanup()
				b.status.BuildFinished()
				*err = "interrupted by user"
				return result.Status
			}
			pendingCommands--
			if !b.FinishCommand(&result, err) {
				b.Cleanup()
				b.status.BuildFinished()
				return result.Status
			}
			if !result.Success() {
				failuresAllowed--
			}
			continue
		}

		// 无法继续
		b.status.BuildFinished()
		if failuresAllowed == 0 {
			if b.config.FailuresAllowed > 1 {
				*err = "subcommands failed"
			} else {
				*err = "subcommand failed"
			}
		} else {
			*err = "stuck [this is a bug]"
		}
		return b.exitCode
	}

	b.status.BuildFinished()
	return ExitSuccess
}

func (b *Builder) StartEdge(edge *Edge, err *string) bool {
	// METRIC_RECORD("StartEdge") - ignored

	if edge.IsPhony() {
		return true
	}

	start_time_millis := GetTimeMillis() - b.start_time_millis_
	b.running_edges_[edge] = int(start_time_millis)

	b.status.BuildEdgeStarted(edge, start_time_millis)

	var buildStart int64
	if b.config.DryRun {
		buildStart = 0
	} else {
		buildStart = -1
	}

	// Create directories necessary for outputs and remember the current
	// filesystem mtime to record later
	// XXX: this will block; do we care?
	for _, o := range edge.Outputs {
		if !b.disk.MakeDirs(o.Path) {
			return false
		}
		if buildStart == -1 {
			b.disk.WriteFile(b.lockFilePath, "", false)
			buildStart = b.disk.Stat(b.lockFilePath, err)
			if buildStart == -1 {
				buildStart = 0
			}
		}
	}

	edge.CommandStartTime = buildStart

	// Create depfile directory if needed.
	depfile := edge.GetUnescapedDepfile()
	if depfile != "" && !b.disk.MakeDirs(depfile) {
		return false
	}

	// Create response file, if needed
	rspfile := edge.GetUnescapedRspfile()
	if rspfile != "" {
		content := edge.GetBinding("rspfile_content")
		if !b.disk.WriteFile(rspfile, content, true) {
			return false
		}
	}

	// start command computing and run it
	if !b.commandRunner.StartCommand(edge) {
		*err = "command '" + edge.EvaluateCommand(false) + "' failed."
		return false
	}

	return true
}

var g_keep_rsp = false

func (b *Builder) FinishCommand(result *CommandResult, err *string) bool {
	// METRIC_RECORD("FinishCommand") - ignored

	edge := result.Edge

	// First try to extract dependencies from the result, if any.
	// This must happen first as it filters the command output (we want
	// to filter /showIncludes output, even on compile failure) and
	// extraction itself can fail, which makes the command fail from a
	// build perspective.
	var depsNodes []*Node
	depsType := edge.GetBinding("deps")
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

	b.status.BuildEdgeFinished(edge, startTimeMillis, endTimeMillis, result.Status, result.Output)

	// The rest of this function only applies to successful commands.
	if !result.Success() {
		return b.plan.EdgeFinished(edge, EdgeFailed, err)
	}

	// Restat the edge outputs
	var recordMtime int64 = 0
	if !b.config.DryRun {
		restat := edge.GetBindingBool("restat")
		generator := edge.GetBindingBool("generator")
		nodeCleaned := false
		recordMtime = edge.CommandStartTime

		// restat and generator rules must restat the outputs after the build
		// has finished. if recordMtime == 0, then there was an error while
		// attempting to touch/stat the temp file when the edge started and
		// we should fall back to recording the outputs' current mtime in the
		// log.
		if recordMtime == 0 || restat || generator {
			for _, o := range edge.Outputs {
				newMtime := b.disk.Stat(o.Path, err)
				if newMtime == -1 {
					return false
				}
				if newMtime > recordMtime {
					recordMtime = newMtime
				}
				if o.Mtime == newMtime && restat {
					// The rule command did not change the output. Propagate the clean
					// state through the build graph.
					// Note that this also applies to nonexistent outputs (mtime == 0).
					if !b.plan.CleanNode(b.scan, o, err) {
						return false
					}
					nodeCleaned = true
				}
			}
		}
		if nodeCleaned {
			recordMtime = edge.CommandStartTime
		}
	}

	if !b.plan.EdgeFinished(edge, EdgeSucceeded, err) {
		return false
	}

	// Delete any left over response file.
	rspfile := edge.GetUnescapedRspfile()
	if rspfile != "" && !g_keep_rsp {
		b.disk.RemoveFile(rspfile)
	}

	if b.scan.buildLog != nil {
		if !b.scan.buildLog.RecordCommand(edge, int(startTimeMillis), int(endTimeMillis), recordMtime) {
			*err = "Error writing to build log: " // Need to handle errno appropriately
			return false
		}
	}

	if depsType != "" && !b.config.DryRun {
		if len(edge.Outputs) == 0 {
			panic("should have been rejected by parser")
		}
		for _, o := range edge.Outputs {
			depsMtime := b.disk.Stat(o.Path, err)
			if depsMtime == -1 {
				return false
			}
			if !b.scan.depsLog.RecordDeps1(o, depsMtime, depsNodes) {
				*err = "Error writing to deps log: "
				return false
			}
		}
	}
	return true
}

// KeepDepfile 控制是否保留 depfile 文件（调试用）
var g_keep_depfile = false

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
			*depsNodes = append(*depsNodes, b.state.GetNode(include, ^uint64(0)))
		}
	} else if depsType == "gcc" {
		depfile := result.Edge.GetUnescapedDepfile()
		if depfile == "" {
			*err = "edge with deps=gcc but no depfile makes no sense"
			return false
		}

		// Read depfile content. Treat a missing depfile as empty.
		var content string
		status := b.disk.ReadFile(depfile, &content, err)
		if status == util.StatusNotFound {
			*err = "" // clear error
		} else if status == util.StatusOtherError {
			return false
		}
		if content == "" {
			return true
		}

		deps := NewDepfileParser(b.config.DepfileParserOptions)
		if !deps.Parse(content, err) {
			return false
		}

		// XXX check depfile matches expected output.
		*depsNodes = make([]*Node, 0, len(deps.Ins))
		for _, input := range deps.Ins {
			// Convert StringPiece to mutable string for canonicalization
			pathStr := input
			slashBits := uint64(0)
			util.CanonicalizePathString(&pathStr, &slashBits) // assume helper exists
			*depsNodes = append(*depsNodes, b.state.GetNode(pathStr, slashBits))
		}

		if !g_keep_depfile {
			if errRemove := b.disk.RemoveFile(depfile); errRemove != 0 {
				*err = "deleting depfile  failed \n"
				return false
			}
		}
	} else {
		panic("unknown deps type '" + depsType + "'")
	}

	return true
}

func (b *Builder) LoadDyndeps(node *Node, err *string) bool {
	// 加载 dyndep 信息
	var ddf DyndepFile
	if !b.scan.LoadDyndeps2(node, &ddf, err) {
		return false
	}
	// 更新构建计划
	if !b.plan.DyndepsLoaded(b.scan, node, ddf, err) {

		return false
	}
	return true
}

// Cleanup 清理中断或失败构建产生的临时文件和部分输出。
func (b *Builder) Cleanup() {
	if b.commandRunner != nil {
		activeEdges := b.commandRunner.GetActiveEdges()
		b.commandRunner.Abort()

		for _, edge := range activeEdges {
			depfile := edge.GetUnescapedDepfile()
			for _, out := range edge.Outputs {
				// 仅当输出文件被实际修改时才删除。对于 generator 规则，我们可能不希望删除清单文件。
				// 但如果规则使用了 depfile，则始终删除（考虑这种情况：由于 depfile 中提到的头文件修改导致需要重建输出，
				// 但命令在触及输出文件之前被中断）。
				var err string
				newMtime := b.disk.Stat(out.Path, &err)
				if newMtime == -1 {
					b.status.Error("%v", err)
				}
				if depfile != "" || out.Mtime != newMtime {
					b.disk.RemoveFile(out.Path)
				}
			}
			if depfile != "" {
				b.disk.RemoveFile(depfile)
			}
		}
	}

	// 删除锁文件
	var err string
	if b.disk.Stat(b.lockFilePath, &err) > 0 {
		b.disk.RemoveFile(b.lockFilePath)
	}
}

// / Set Jobserver client instance for this builder.
func (b *Builder) SetJobserverClient(jobserver_client JobserverClient) {
	b.jobserver_ = jobserver_client
}
