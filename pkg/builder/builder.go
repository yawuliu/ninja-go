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
	runningEdges  map[*Edge]time.Time
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

func (b *Builder) AddTargetByName(name string) (*Node, error) {
	node := b.state.LookupNode(name)
	if node == nil {
		return nil, fmt.Errorf("unknown target: '%s'", name)
	}
	if succ, err := b.AddTarget(node); !succ && err != nil {
		return nil, err
	}

	return node, nil
}

func (b *Builder) AddTarget(target *Node) (bool, error) {
	var validationNodes []*Node
	if succ, err := b.scan.RecomputeDirty(target, &validationNodes); !succ && err != nil {
		return false, err
	}
	if edge := target.InEdge; edge == nil || !edge.OutputsReady {
		if succ, err := b.plan.AddTarget(target); !succ {
			return false, err
		}
	}
	for _, vn := range validationNodes {
		if e := vn.InEdge; e != nil && !e.OutputsReady {
			if succ, err := b.plan.AddTarget(vn); !succ {
				return false, err
			}
		}
	}
	return true, nil
}
func (b *Builder) AlreadyUpToDate() bool {
	return !b.plan.MoreToDo()
}

func (b *Builder) StartEdge(edge *Edge) error {
	// 启动命令
	return b.commandRunner.StartCommand(edge)
}

func (b *Builder) Build() (ExitStatus, error) {
	if b.AlreadyUpToDate() {
		return ExitSuccess, nil
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
				if err := b.startEdge(edge); err != nil {
					b.Cleanup()
					b.status.BuildFinished()
					return ExitFailure, err
				}
				if edge.IsPhony() {
					if err := b.plan.EdgeFinished(edge, EdgeSucceeded); err != nil {
						b.Cleanup()
						b.status.BuildFinished()
						return ExitFailure, err
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
			result, err := b.commandRunner.WaitForCommand()
			if err != nil || result.Status == ExitInterrupted {
				b.Cleanup()
				b.status.BuildFinished()
				return result.Status, fmt.Errorf("interrupted by user")
			}
			pendingCommands--
			if err := b.finishCommand(result); err != nil {
				b.Cleanup()
				b.status.BuildFinished()
				return result.Status, err
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
				return b.exitCode, fmt.Errorf("subcommands failed")
			}
			return b.exitCode, fmt.Errorf("subcommand failed")
		}
		return b.exitCode, fmt.Errorf("cannot make progress due to previous errors")
	}

	b.status.BuildFinished()
	return b.exitCode, nil
}

func (b *Builder) startEdge(edge *Edge) error {
	if edge.IsPhony() {
		return nil
	}
	start := (time.Now().UnixNano() - b.startTimeMillis) / 1e6
	b.status.BuildEdgeStarted(edge, start)

	// 创建输出目录
	for _, out := range edge.Outputs {
		if err := b.disk.MkdirAll(util.DirName(out.Path), 0755); err != nil {
			return err
		}
	}
	// 创建响应文件
	rspfile := edge.GetBinding("rspfile")
	if rspfile != "" {
		content := edge.GetBinding("rspfile_content")
		if err := b.disk.WriteFile(rspfile, []byte(content), 0644); err != nil {
			return err
		}
	}
	// 启动命令
	if err := b.commandRunner.StartCommand(edge); err != nil {
		return fmt.Errorf("command '%s' failed: %v", edge.EvaluateCommand(false), err)
	}
	return nil
}

var g_keep_rsp = false

func (b *Builder) finishCommand(result *CommandResult) error {
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
		return fmt.Errorf("edge %v not found in running edges", edge)
	}
	endMillis := time.Now().UnixNano()/1e6 - b.startTimeMillis
	delete(b.runningEdges, edge)

	// 通知状态
	b.status.BuildEdgeFinished(edge, startMillis.UnixMilli(), endMillis, result.Status, result.Output)

	// 如果命令失败，直接标记边失败并返回
	if result.Status != ExitSuccess {
		return b.plan.EdgeFinished(edge, EdgeFailed)
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
				newMtime, err := b.disk.Stat(out.Path)
				if err != nil {
					return err
				}
				if newMtime.ModTime().UnixNano() > recordMtime {
					recordMtime = newMtime.ModTime().UnixNano()
				}
				if restat && out.Mtime == newMtime.ModTime().UnixNano() {
					if err := b.plan.CleanNode(b.scan, out); err != nil {
						return err
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
	if err := b.plan.EdgeFinished(edge, EdgeSucceeded); err != nil {
		return err
	}

	// 删除响应文件
	rspfile := edge.GetUnescapedRspfile()
	if rspfile != "" && !g_keep_rsp {
		if err := b.disk.Remove(rspfile); err != nil && !os.IsNotExist(err) {
			// 忽略不存在的文件错误
		}
	}

	// 记录构建日志
	if b.buildLog != nil {
		if err := b.buildLog.RecordCommand(edge, startMillis.UnixMilli(), endMillis, recordMtime); err != nil {
			return fmt.Errorf("error writing to build log: %v", err)
		}
	}

	// 记录依赖日志
	if depsType != "" && !b.config.DryRun {
		if len(edge.Outputs) == 0 {
			return fmt.Errorf("edge with deps but no outputs")
		}
		for _, out := range edge.Outputs {
			depsMtime, err := b.disk.Stat(out.Path)
			if err != nil {
				return err
			}
			if err := b.depsLog.RecordDeps(out, depsMtime.ModTime().UnixNano(), depsNodes); err != nil {
				return fmt.Errorf("error writing to deps log: %v", err)
			}
		}
	}

	return nil
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
			if err := b.disk.Remove(depfile); err != nil {
				return fmt.Errorf("deleting depfile %s: %v", depfile, err)
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown deps type '%s'", depsType)
	}
}

func (b *Builder) LoadDyndeps(node *Node) error {
	// 加载 dyndep 信息
	var ddf DyndepFile
	err := b.scan.LoadDyndeps2(node, &ddf)
	if err != nil {
		return err
	}
	// 更新构建计划
	if err := b.plan.DyndepsLoaded(b.scan, node, ddf); err != nil {
		return err
	}
	return nil
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
				newMtime, err := b.disk.Stat(out.Path)
				if err != nil {
					// 忽略 Stat 错误，仅记录
					b.status.Error("%v", err)
				}
				if depfile != "" || out.Mtime != newMtime.ModTime().UnixNano() {
					b.disk.Remove(out.Path)
				}
			}
			if depfile != "" {
				b.disk.Remove(depfile)
			}
		}
	}

	// 删除锁文件
	if _, err := b.disk.Stat(b.lockFilePath); err == nil {
		b.disk.Remove(b.lockFilePath)
	}
}

// / Set Jobserver client instance for this builder.
func (b *Builder) SetJobserverClient(jobserver_client JobserverClient) {
	b.jobserver_ = jobserver_client
}
