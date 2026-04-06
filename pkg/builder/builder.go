package builder

import (
	"errors"
	"fmt"
	"ninja-go/pkg/buildlog"
	"ninja-go/pkg/depslog"
	"ninja-go/pkg/graph"
	"ninja-go/pkg/plan"
	"ninja-go/pkg/util"
	"os"
	"path"
	"time"
)

// Builder 主构建器
type Builder struct {
	state         *graph.State
	config        *BuildConfig
	buildLog      *buildlog.BuildLog
	depsLog       *depslog.DepsLog
	runningEdges  map[*graph.Edge]time.Time
	disk          util.FileSystem
	plan          *plan.Plan
	status        Status
	startTime     int64
	exitCode      ExitStatus
	explanations  *Explanations
	scan          *graph.DependencyScan
	commandRunner CommandRunner
	lockFilePath  string
}

func NewBuilder(state *graph.State, config *BuildConfig, buildLog *buildlog.BuildLog,
	depsLog *depslog.DepsLog, disk_interface util.FileSystem, status Status, start_time_millis int64) *Builder {
	b := &Builder{
		state:        state,
		config:       config,
		buildLog:     buildLog,
		depsLog:      depsLog,
		disk:         disk_interface,
		status:       status,
		startTime:    start_time_millis,
		lockFilePath: ".ninja_lock",
	}
	b.lockFilePath = ".ninja_lock"
	build_dir := state.Bindings.LookupVariable("builddir")
	if build_dir != "" {
		b.lockFilePath = path.Join(build_dir, b.lockFilePath)
	}
	b.plan = plan.NewPlan(b)
	b.scan = graph.NewDependencyScan(state, buildLog, depsLog, disk_interface,
		config.DepfileParserOptions, b.explanations)
	return b
}

func (b *Builder) Destruct() {
	b.Cleanup()
	b.status.SetExplanations(nil)
}

func (b *Builder) AddTarget(target *graph.Node) error {
	var validationNodes []*graph.Node
	if err := b.scan.RecomputeDirty(target, &validationNodes); err != nil {
		return err
	}
	if edge := target.InEdge; edge == nil || !edge.OutputsReady {
		if err := b.plan.AddTarget(target); err != nil {
			return err
		}
	}
	for _, vn := range validationNodes {
		if e := vn.InEdge; e != nil && !e.OutputsReady {
			if err := b.plan.AddTarget(vn); err != nil {
				return err
			}
		}
	}
	return nil
}
func (b *Builder) AlreadyUpToDate() bool {
	return !b.plan.MoreToDo()
}

func (b *Builder) StartEdge(edge *graph.Edge) error {
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
			b.commandRunner = NewRealCommandRunner(b.config.Parallelism)
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
					if err := b.plan.EdgeFinished(edge, plan.EdgeSucceeded); err != nil {
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

func (b *Builder) startEdge(edge *graph.Edge) error {
	if edge.IsPhony() {
		return nil
	}
	start := (time.Now().UnixNano() - b.startTime) / 1e6
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

func (b *Builder) finishCommand(result *CommandResult) error {
	edge := result.Edge
	start := result.StartTime // 需要从 runner 获取
	end := (time.Now().UnixNano() - b.startTime) / 1e6
	b.status.BuildEdgeFinished(edge, start, end, result.Status, result.Output)

	if !result.Success() {
		return b.plan.EdgeFinished(edge, plan.EdgeFailed)
	}

	// 处理 deps（msvc/gcc）
	depsType := edge.GetBinding("deps")
	if depsType != "" && !b.config.DryRun {
		var depsNodes []*graph.Node
		if err := b.extractDeps(result, depsType, &depsNodes); err != nil {
			return err
		}
		for _, out := range edge.Outputs {
			mtime, _ := b.disk.Stat(out.Path)
			if err := b.depsLog.RecordDeps(out, mtime.UnixNano(), depsNodes); err != nil {
				return err
			}
		}
	}

	// restat 处理
	restat := edge.GetBindingBool("restat")
	generator := edge.GetBindingBool("generator")
	recordMtime := edge.CommandStartTime
	if recordMtime == 0 || restat || generator {
		for _, out := range edge.Outputs {
			info, err := b.disk.Stat(out.Path)
			if err == nil && info.ModTime().UnixNano() > recordMtime {
				recordMtime = info.ModTime().UnixNano()
			}
			if restat && out.Mtime == info.ModTime().UnixNano() {
				if err := b.plan.CleanNode(b.scan, out); err != nil {
					return err
				}
			}
		}
	}

	if err := b.plan.EdgeFinished(edge, plan.EdgeSucceeded); err != nil {
		return err
	}

	// 记录 build log
	if b.buildLog != nil {
		b.buildLog.RecordCommand(edge, start, end)
	}

	// 删除响应文件
	rspfile := edge.GetBinding("rspfile")
	if rspfile != "" {
		b.disk.Remove(rspfile)
	}
	return nil
}

// KeepDepfile 控制是否保留 depfile 文件（调试用）
var g_keep_depfile = false

func (b *Builder) extractDeps(result *CommandResult, depsType, depsPrefix string, depsNodes *[]*graph.Node) error {
	switch depsType {
	case "msvc":
		parser := NewCLParser()
		filteredOutput, includes, err := parser.Parse(result.Output, depsPrefix, b.workingDir)
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

func (b *Builder) LoadDyndeps(node *graph.Node) error {
	// 加载 dyndep 信息
	ddf, err := b.scan.LoadDyndeps(node)
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
				if depfile != "" || out.Mtime != newMtime.UnixNano() {
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
