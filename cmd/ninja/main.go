package main

import (
	"fmt"
	"ninja-go/pkg/builder"
	"ninja-go/pkg/util"
	"os"
	"runtime"
	"strconv"
	"strings"
)

var (
	// 全局调试标志
	g_metrics                *builder.Metrics
	g_explaining             bool
	g_keep_depfile           bool
	g_keep_rsp               bool
	g_experimental_statcache = true
)

func main() {
	os.Exit(realMain())
}

const kNinjaVersion = "1.14.0.git"

type NinjaMain struct {
	builder.BuildLogUser
	/// Command line used to run Ninja.
	ninja_command_ string

	/// Build configuration set from flags (e.g. parallelism).
	config_ *builder.BuildConfig

	/// Loaded state (rules, nodes).
	state_ builder.State

	/// Functions for accessing the disk.
	disk_interface_ util.FileSystem

	/// The build directory, used for storing the build log etc.
	build_dir_ string

	build_log_         *builder.BuildLog
	deps_log_          *builder.DepsLog
	start_time_millis_ int64
}

// RebuildManifest 如果必要则重新构建清单文件。
// 返回 true 表示清单已被重建。
func (n *NinjaMain) RebuildManifest(inputFile string, status builder.Status) (bool, error) {
	path := inputFile
	if path == "" {
		return false, fmt.Errorf("empty path")
	}
	norm, _ := util.CanonicalizePath(path)
	node := n.state_.LookupNode(norm)
	if node == nil {
		return false, nil
	}

	builder := builder.NewBuilder(&n.state_, n.config_, n.build_log_, n.deps_log_, n.start_time_millis_, n.disk_interface_, status)
	if err := builder.AddTarget(node); err != nil {
		return false, err
	}

	if builder.AlreadyUpToDate() {
		return false, nil // 没有重建
	}

	if _, err := builder.Build(); err != nil {
		return false, err
	}

	// 只有当节点现在变脏时才认为清单被重建（可能被 restat 清理）
	if !node.Dirty {
		// 重置状态以避免问题（如 https://github.com/ninja-build/ninja/issues/874）
		n.state_.Reset()
		return false, nil
	}

	return true, nil
}

// ParsePreviousElapsedTimes 从构建日志中加载每条边上次构建的耗时，用于 ETA 预测。
func (n *NinjaMain) ParsePreviousElapsedTimes() {
	for _, edge := range n.state_.Edges {
		for _, out := range edge.Outputs {
			logEntry := n.build_log_.LookupByOutput(out.Path)
			if logEntry == nil {
				continue // 可能该边的其他输出有记录，继续检查下一个输出
			}
			edge.PrevElapsedTimeMillis = logEntry.EndTime - logEntry.StartTime
			break // 只要找到一个输出有记录即可，继续下一条边
		}
	}
}

// CollectTarget 将命令行路径转换为 Node，支持特殊语法 "foo.cc^"（取第一个输出）。
func (n *NinjaMain) CollectTarget(cpath string) (*builder.Node, error) {
	path := cpath
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	norm, slashBits := util.CanonicalizePath(path)

	// 特殊语法：以 '^' 结尾表示取该节点的第一个输出
	firstDependent := false
	if len(norm) > 0 && norm[len(norm)-1] == '^' {
		norm = norm[:len(norm)-1]
		firstDependent = true
	}

	node := n.state_.LookupNode(norm)
	if node != nil {
		if firstDependent {
			if len(node.OutEdges) == 0 {
				// 没有出边，尝试从 deps log 中查找反向依赖
				revNode := n.deps_log_.GetFirstReverseDepsNode(node)
				if revNode == nil {
					return nil, fmt.Errorf("'%s' has no out edge", norm)
				}
				node = revNode
			} else {
				edge := node.OutEdges[0]
				if len(edge.Outputs) == 0 {
					// 不应发生，防御性代码
					return nil, fmt.Errorf("edge has no outputs")
				}
				node = edge.Outputs[0]
			}
		}
		return node, nil
	}

	// 节点不存在，构建错误信息
	decanon := util.PathDecanonicalized(norm, slashBits)
	errMsg := fmt.Sprintf("unknown target '%s'", decanon)
	if norm == "clean" {
		errMsg += ", did you mean 'ninja -t clean'?"
	} else if norm == "help" {
		errMsg += ", did you mean 'ninja -h'?"
	} else {
		suggestion := n.state_.SpellcheckNode(norm)
		if suggestion != nil {
			errMsg += fmt.Sprintf(", did you mean '%s'?", suggestion.Path)
		}
	}
	return nil, fmt.Errorf("%s", errMsg)
}

// CollectTargetsFromArgs 将命令行参数转换为 Node 列表，如果无参数则使用默认目标。
func (n *NinjaMain) CollectTargetsFromArgs(args []string) ([]*builder.Node, error) {
	if len(args) == 0 {
		defaults := n.state_.DefaultNodes()
		if len(defaults) == 0 {
			return nil, fmt.Errorf("no default nodes specified")
		}
		return defaults, nil
	}

	var targets []*builder.Node
	for _, arg := range args {
		node, err := n.CollectTarget(arg)
		if err != nil {
			return nil, err
		}
		targets = append(targets, node)
	}
	return targets, nil
}

// / Command-line options.
type Options struct {
	/// Build file to load.
	input_file string

	/// Directory to change into before running.
	working_dir string

	/// Tool to run rather than building.
	tool *Tool

	/// Whether phony cycles should warn or print an error.
	phony_cycle_should_err bool
}

// ToolGraph 输出 graphviz dot 文件（用于可视化依赖图）。
func (n *NinjaMain) ToolGraph(options *Options, args []string) int {
	nodes, err := n.CollectTargetsFromArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}

	graph := graphviz.NewGraph(n.state_, n.disk_interface_)
	graph.Start()
	for _, node := range nodes {
		graph.AddTarget(node)
	}
	graph.Finish()
	return 0
}

func realMain() int {
	// 构建配置
	config := builder.DefaultBuildConfig()
	options := struct {
		inputFile           string
		workingDir          string
		tool                string
		phonyCycleShouldErr bool
	}{
		inputFile: "build.ninja",
	}

	// 自定义 flag 解析（因为需要支持 -d, -w 等多次出现）
	args := os.Args[1:]
	var positionalArgs []string
	// 手动解析，支持 -t tool 后的参数
	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			usage(config)
			return 0
		case arg == "--version":
			fmt.Println(kNinjaVersion)
			return 0
		case arg == "-v" || arg == "--verbose":
			config.Verbosity = builder.VerbosityVerbose
		case arg == "--quiet":
			config.Verbosity = builder.VerbosityNoStatusUpdate
		case arg == "-n":
			config.DryRun = true
			config.DisableJobserverClient = true
		case arg == "-d":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "ninja: -d requires an argument\n")
				return 1
			}
			mode := args[i+1]
			if !debugEnable(mode) {
				return 1
			}
			i++
		case arg == "-w":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "ninja: -w requires an argument\n")
				return 1
			}
			warning := args[i+1]
			if !warningEnable(warning, &options) {
				return 1
			}
			i++
		case arg == "-C":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "ninja: -C requires a directory\n")
				return 1
			}
			options.workingDir = args[i+1]
			i++
		case arg == "-f":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "ninja: -f requires a file\n")
				return 1
			}
			options.inputFile = args[i+1]
			i++
		case arg == "-j":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "ninja: -j requires a number\n")
				return 1
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil || val < 0 {
				fmt.Fprintf(os.Stderr, "ninja: invalid -j parameter\n")
				return 1
			}
			if val == 0 {
				config.Parallelism = int(^uint(0) >> 1) // MaxInt
			} else {
				config.Parallelism = val
			}
			config.DisableJobserverClient = true
			i++
		case arg == "-k":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "ninja: -k requires a number\n")
				return 1
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil || val < 0 {
				fmt.Fprintf(os.Stderr, "ninja: invalid -k parameter\n")
				return 1
			}
			if val == 0 {
				config.FailuresAllowed = int(^uint(0) >> 1)
			} else {
				config.FailuresAllowed = val
			}
			i++
		case arg == "-l":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "ninja: -l requires a number\n")
				return 1
			}
			val, err := strconv.ParseFloat(args[i+1], 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ninja: invalid -l parameter\n")
				return 1
			}
			config.MaxLoadAverage = val
			i++
		case arg == "-t":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "ninja: -t requires a tool name\n")
				return 1
			}
			options.tool = args[i+1]
			i++
			// 剩余参数全部作为工具的参数
			positionalArgs = args[i+1:]
			i = len(args) // 结束循环
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(os.Stderr, "ninja: unknown option %s\n", arg)
				return 1
			}
			positionalArgs = append(positionalArgs, arg)
		}
		i++
	}

	// 处理工具模式
	if options.tool != "" {
		// 工具会在加载状态后运行，但有些工具需要提前运行
		// 简化：所有工具都先加载状态
		// 具体工具实现后续补充
		fmt.Fprintf(os.Stderr, "ninja: tool '%s' not implemented yet\n", options.tool)
		return 1
	}

	// 改变工作目录
	if options.workingDir != "" {
		if err := os.Chdir(options.workingDir); err != nil {
			fmt.Fprintf(os.Stderr, "ninja: chdir to '%s': %v\n", options.workingDir, err)
			return 1
		}
	}

	// 加载构建文件
	state := builder.NewState()
	diskInterface := builder.NewRealFileSystem()
	parserOpts := builder.ManifestParserOptions{
		PhonyCycleAction: builder.PhonyCycleActionWarn,
	}
	if options.phonyCycleShouldErr {
		parserOpts.PhonyCycleAction = builder.PhonyCycleActionError
	}
	manifestParser := builder.NewManifestParser(state, diskInterface, parserOpts)
	if err := manifestParser.Load(options.inputFile, nil); err != nil {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}

	// 确保构建目录存在
	buildDir := state.Bindings.LookupVariable("builddir")
	if buildDir != "" && !config.DryRun {
		if err := diskInterface.MakeDirs(buildDir + "/."); err != nil && !os.IsExist(err) {
			fmt.Fprintf(os.Stderr, "ninja: creating build directory %s: %v\n", buildDir, err)
			return 1
		}
	}

	// 打开日志
	log_path := ".ninja_log"
	if buildDir != "" {
		log_path = buildDir + "/" + log_path
	}
	buildLog := builder.NewBuildLog(log_path)
	if buildDir != "" {
		buildLog = builder.NewBuildLog(buildDir + "/.ninja_log")
	}
	if err := buildLog.Load(log_path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "ninja: loading build log: %v\n", err)
		return 1
	}
	if !config.DryRun {
		if err := buildLog.OpenForWrite(log_path, this); err != nil {
			fmt.Fprintf(os.Stderr, "ninja: opening build log: %v\n", err)
			return 1
		}
		defer buildLog.Close()
	}

	path := ".ninja_deps"
	if buildDir != "" {
		path = buildDir + "/" + path
	}
	depsLog := builder.NewDepsLog(path)
	if buildDir != "" {
		depsLog = builder.NewDepsLog(buildDir + "/.ninja_deps")
	}
	if err := depsLog.Load(state); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "ninja: loading deps log: %v\n", err)
		return 1
	}
	if !config.DryRun {
		if err := depsLog.OpenForWrite(path); err != nil {
			fmt.Fprintf(os.Stderr, "ninja: opening deps log: %v\n", err)
			return 1
		}
		defer depsLog.Close()
	}

	// 准备构建目标
	var targets []*builder.Node
	if len(positionalArgs) == 0 {
		defaults := state.DefaultNodes()
		if len(defaults) == 0 {
			fmt.Fprintf(os.Stderr, "ninja: defaults not found\n")
			return 1
		}
		targets = defaults
	} else {
		for _, arg := range positionalArgs {
			node := state.LookupNode(arg)
			if node == nil {
				fmt.Fprintf(os.Stderr, "ninja: unknown target '%s'\n", arg)
				return 1
			}
			targets = append(targets, node)
		}
	}

	// 创建状态输出
	statusPrinter := builder.NewConsoleStatus(config)
	start_time_millis_ := int64(0)
	// 创建 Builder
	b := builder.NewBuilder(state, &config, buildLog, depsLog, start_time_millis_, diskInterface, statusPrinter)

	// 添加目标
	for _, t := range targets {
		if err := b.AddTarget(t); err != nil {
			fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
			return 1
		}
	}

	// 构建
	if b.AlreadyUpToDate() {
		if config.Verbosity != builder.VerbosityNoStatusUpdate {
			fmt.Println("ninja: no work to do.")
		}
		return 0
	}

	exitStatus, err := b.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ninja: build stopped: %v.\n", err)
	}
	return int(exitStatus)
}

func usage(config builder.BuildConfig) {
	fmt.Fprintf(os.Stderr, `usage: ninja [options] [targets...]

if targets are unspecified, builds the 'default' target (see manual).

options:
  --version      print ninja version ("%s")
  -v, --verbose  show all command lines while building
  --quiet        don't show progress status, just command output

  -C DIR   change to DIR before doing anything else
  -f FILE  specify input build file [default=build.ninja]

  -j N     run N jobs in parallel (0 means infinity) [default=%d on this system]
  -k N     keep going until N jobs fail (0 means infinity) [default=1]
  -l N     do not start new jobs if the load average is greater than N
  -n       dry run (don't run commands but act like they succeeded)

  -d MODE  enable debugging (use '-d list' to list modes)
  -t TOOL  run a subtool (use '-t list' to list subtools)
    terminates toplevel options; further flags are passed to the tool
  -w FLAG  adjust warnings (use '-w list' to list warnings)
`, kNinjaVersion, guessParallelism())
}

func guessParallelism() int {
	n := runtime.NumCPU()
	if n <= 1 {
		return 2
	}
	if n == 2 {
		return 3
	}
	return n + 2
}

func debugEnable(mode string) bool {
	switch mode {
	case "list":
		fmt.Println(`debugging modes:
  stats        print operation counts/timing info
  explain      explain what caused a command to execute
  keepdepfile  don't delete depfiles after they're read by ninja
  keeprsp      don't delete @response files on success
  nostatcache  don't batch stat() calls per directory and cache them`)
		return false
	case "stats":
		g_metrics = &builder.Metrics{}
		return true
	case "explain":
		g_explaining = true
		return true
	case "keepdepfile":
		g_keep_depfile = true
		return true
	case "keeprsp":
		g_keep_rsp = true
		return true
	case "nostatcache":
		g_experimental_statcache = false
		return true
	default:
		fmt.Fprintf(os.Stderr, "ninja: unknown debug setting '%s'\n", mode)
		return false
	}
}

func warningEnable(name string, options *struct {
	inputFile           string
	workingDir          string
	tool                string
	phonyCycleShouldErr bool
}) bool {
	switch {
	case name == "list":
		fmt.Println(`warning flags:
  phonycycle={err,warn}  phony build statement references itself`)
		return false
	case name == "phonycycle=err":
		options.phonyCycleShouldErr = true
		return true
	case name == "phonycycle=warn":
		options.phonyCycleShouldErr = false
		return true
	default:
		fmt.Fprintf(os.Stderr, "ninja: unknown warning flag '%s'\n", name)
		return false
	}
}
