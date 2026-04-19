package builder

import (
	"errors"
	"fmt"
	"ninja-go/pkg/util"
	"os"
	"path"
	"time"
)

// Builder 主构建器
type Builder struct {
	state         *State
	config        *BuildConfig
	buildLog      *BuildLog
	depsLog       *DepsLog
	runningEdges  map[*Edge]int64
	disk          util.FileSystem
	plan          *Plan
	status        Status
	exitCode      ExitStatus
	explanations  *OptionalExplanations
	scan          *DependencyScan
	commandRunner CommandRunner
	lockFilePath  string
	jobserver_    JobserverClient
	/// Time the build started.
	startTimeMillis int64
}

func NewBuilder(state *State, config *BuildConfig, buildLog *BuildLog,
	depsLog *DepsLog, start_time_millis int64,
	disk_interface util.FileSystem, status Status) *Builder {
	b := &Builder{
		state:           state,
		config:          config,
		buildLog:        buildLog,
		depsLog:         depsLog,
		disk:            disk_interface,
		status:          status,
		startTimeMillis: start_time_millis,
		lockFilePath:    ".ninja_lock",
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

func (b *Builder) StartEdge(edge *Edge, err *string) bool {
	// 启动命令
	// METRIC_RECORD("StartEdge") - ignored in Go conversion

	if edge.IsPhony() {
		return true
	}

	startTimeMillis := GetTimeMillis() - b.startTimeMillis
	b.runningEdges[edge] = startTimeMillis

	b.status.BuildEdgeStarted(edge, startTimeMillis)

	var buildStart int64
	if b.config.DryRun {
		buildStart = 0
	} else {
		buildStart = -1
	}

	// Create directories necessary for outputs and remember the current filesystem mtime to record later
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
				if !b.startEdge(edge, err) {
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
			if !b.finishCommand(&result, err) {
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

func (b *Builder) startEdge(edge *Edge, err *string) bool {
	if edge.IsPhony() {
		return true
	}
	start := (time.Now().UnixNano() - b.startTimeMillis) / 1e6
	b.status.BuildEdgeStarted(edge, start)

	// 创建输出目录
	for _, out := range edge.Outputs {
		if !b.disk.MakeDirs(util.DirName(out.Path)) {
			return false
		}
	}
	// 创建响应文件
	rspfile := edge.GetBinding("rspfile")
	if rspfile != "" {
		content := edge.GetBinding("rspfile_content")
		if !b.disk.WriteFile(rspfile, content, true) {
			return false
		}
	}
	// 启动命令
	if !b.commandRunner.StartCommand(edge) {
		*err = fmt.Sprintf("command '%s' failed", edge.EvaluateCommand(false))
		return false
	}
	return true
}

var g_keep_rsp = false

func (b *Builder) finishCommand(result *CommandResult, err *string) bool {
	edge := result.Edge

	// 首先提取依赖（必须在命令输出被过滤前执行）
	var depsNodes []*Node
	depsType := edge.GetBinding("deps")
	depsPrefix := edge.GetBinding("msvc_deps_prefix")
	if depsType != "" {
		extractErr := b.extractDeps(result, depsType, depsPrefix, &depsNodes)
		if extractErr != nil && result.Status == ExitSuccess {
			if result.Output != "" {
				result.Output += "\n"
			}
			result.Output += extractErr.Error()
			result.Status = ExitFailure
		}
	}

	// 获取边的开始和结束时间
	startMillis, ok := b.runningEdges[edge]
	if !ok {
		*err = fmt.Sprintf("edge %v not found in running edges", edge)
		return false
	}
	endMillis := time.Now().UnixNano()/1e6 - b.startTimeMillis
	delete(b.runningEdges, edge)

	// 通知状态
	b.status.BuildEdgeFinished(edge, startMillis, endMillis, result.Status, result.Output)

	// 如果命令失败，直接标记边失败并返回
	if result.Status != ExitSuccess {
		return b.plan.EdgeFinished(edge, EdgeFailed, err)
	}

	// 处理 restat 和 generator 规则
	recordMtime := int64(0)
	if !b.config.DryRun {
		restat := edge.GetBindingBool("restat")
		generator := edge.GetBindingBool("generator")
		nodeCleaned := false
		recordMtime = edge.CommandStartTime

		if recordMtime == 0 || restat || generator {
			for _, out := range edge.Outputs {
				newMtime := b.disk.Stat(out.Path, err)
				if newMtime == -1 {
					return false
				}
				if newMtime > recordMtime {
					recordMtime = newMtime
				}
				if out.Mtime == newMtime && restat {
					if !b.plan.CleanNode(b.scan, out, err) {
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

	// 标记边成功完成
	if !b.plan.EdgeFinished(edge, EdgeSucceeded, err) {
		return false
	}

	// 删除响应文件
	rspfile := edge.GetUnescapedRspfile()
	if rspfile != "" && !g_keep_rsp {
		b.disk.RemoveFile(rspfile)
	}

	// 记录构建日志
	if b.buildLog != nil {
		if !b.buildLog.RecordCommand(edge, startMillis, endMillis, recordMtime) {
			*err = fmt.Sprintf("error writing to build log: %v", err)
			return false
		}
	}

	// 记录依赖日志
	if depsType != "" && !b.config.DryRun {
		if len(edge.Outputs) == 0 {
			panic("should have been rejected by parser")
		}
		for _, out := range edge.Outputs {
			depsMtime := b.disk.Stat(out.Path, err)
			if depsMtime == -1 {
				return false
			}
			if !b.depsLog.RecordDeps(out, depsMtime, depsNodes) {
				*err = fmt.Sprintf("error writing to deps log")
				return false
			}
		}
	}

	return true
}

// KeepDepfile 控制是否保留 depfile 文件（调试用）
var g_keep_depfile = false

func (b *Builder) extractDeps(result *CommandResult, depsType, depsPrefix string, depsNodes *[]*Node) error {
	switch depsType {
	case "msvc":
		parser := NewCLParser()
		filteredOutput, includes, err := parser.Parse(result.Output, depsPrefix)
		if err != nil {
			return err
		}
		result.Output = filteredOutput
		// 将所有 includes 添加到 depsNodes，slash_bits 全为 1（表示所有反斜杠都保留）
		for _, inc := range includes {
			node := b.state.GetNode(inc, ^uint64(0))
			*depsNodes = append(*depsNodes, node)
		}
		return nil

	case "gcc":
		depfile := result.Edge.GetUnescapedDepfile()
		if depfile == "" {
			return errors.New("edge with deps=gcc but no depfile makes no sense")
		}

		// 读取 depfile 内容，缺失时视为空
		content, err := b.disk.ReadFile(depfile)
		if err != nil {
			if os.IsNotExist(err) {
				// 文件不存在，无依赖可提取
				return nil
			}
			return fmt.Errorf("reading depfile %s: %v", depfile, err)
		}
		if len(content) == 0 {
			return nil
		}

		parser := &DepfileParser{}
		if err := parser.Parse(string(content)); err != nil {
			return fmt.Errorf("parsing depfile %s: %v", depfile, err)
		}

		// 将依赖节点添加到 depsNodes
		for _, in := range parser.Ins {
			// 规范化路径并获取节点
			norm, slashBits := util.CanonicalizePath(in)
			node := b.state.GetNode(norm, slashBits)
			*depsNodes = append(*depsNodes, node)
		}

		// 如果不保留 depfile，则删除它
		if !g_keep_depfile {
			if b.disk.RemoveFile(depfile) < 0 {
				return fmt.Errorf("deleting depfile %s: %v", depfile, err)
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown deps type '%s'", depsType)
	}
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
