package main

import (
	"fmt"
	"ninja-go/pkg/builder"
	"ninja-go/pkg/util"
	"os"
	"runtime"
	"sort"
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

// ToolQuery 显示指定目标的输入、输出和验证信息。
func (n *NinjaMain) ToolQuery(options *Options, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "ninja: expected a target to query\n")
		return 1
	}

	dyndepLoader := builder.NewDyndepLoader(n.state_, n.disk_interface_)

	for _, arg := range args {
		node, err := n.CollectTarget(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
			return 1
		}

		fmt.Printf("%s:\n", node.Path)
		if edge := node.InEdge; edge != nil {
			// 如果边有挂起的 dyndep 文件，尝试加载
			if edge.DyndepFile != nil && edge.DyndepFile.DyndepPending {
				if err := dyndepLoader.LoadDyndeps(edge.DyndepFile); err != nil {
					fmt.Fprintf(os.Stderr, "ninja: warning: %v\n", err)
				}
			}
			fmt.Printf("  input: %s\n", edge.Rule.Name)
			for idx, in := range edge.Inputs {
				label := ""
				if edge.IsImplicit(idx) {
					label = "| "
				} else if edge.IsOrderOnly(idx) {
					label = "|| "
				}
				fmt.Printf("    %s%s\n", label, in.Path)
			}
			if len(edge.Validations) > 0 {
				fmt.Printf("  validations:\n")
				for _, v := range edge.Validations {
					fmt.Printf("    %s\n", v.Path)
				}
			}
		}
		fmt.Printf("  outputs:\n")
		for _, outEdge := range node.OutEdges {
			for _, out := range outEdge.Outputs {
				fmt.Printf("    %s\n", out.Path)
			}
		}
		if validationEdges := node.ValidationOutEdges; len(validationEdges) > 0 {
			fmt.Printf("  validation for:\n")
			for _, outEdge := range validationEdges {
				for _, out := range outEdge.Outputs {
					fmt.Printf("    %s\n", out.Path)
				}
			}
		}
	}
	return 0
}

// ToolTargetsList 递归打印节点树（深度限制）。
func ToolTargetsList(nodes []*builder.Node, depth, indent int) {
	for _, n := range nodes {
		for i := 0; i < indent; i++ {
			fmt.Print("  ")
		}
		if edge := n.InEdge; edge != nil {
			fmt.Printf("%s: %s\n", n.Path, edge.Rule.Name)
			if depth > 1 || depth <= 0 {
				ToolTargetsList(edge.Inputs, depth-1, indent+1)
			}
		} else {
			fmt.Printf("%s\n", n.Path)
		}
	}
}

// ToolTargetsSourceList 打印所有源文件（没有入边的节点）。
func ToolTargetsSourceList(state *builder.State) {
	for _, edge := range state.Edges {
		for _, in := range edge.Inputs {
			if in.InEdge == nil {
				fmt.Println(in.Path)
			}
		}
	}
}

// ToolTargetsListByRule 打印指定规则生成的所有输出。
func ToolTargetsListByRule(state *builder.State, ruleName string) {
	outputs := make(map[string]bool)
	for _, edge := range state.Edges {
		if edge.Rule.Name == ruleName {
			for _, out := range edge.Outputs {
				outputs[out.Path] = true
			}
		}
	}
	for out := range outputs {
		fmt.Println(out)
	}
}

// ToolTargetsListAll 打印所有输出及其所属规则。
func ToolTargetsListAll(state *builder.State) {
	for _, edge := range state.Edges {
		for _, out := range edge.Outputs {
			fmt.Printf("%s: %s\n", out.Path, edge.Rule.Name)
		}
	}
}

// ToolBrowse 启动浏览器查看依赖图（如果平台支持）。
func (n *NinjaMain) ToolBrowse(options *Options, args []string) int {
	// 模拟条件编译：如果构建时未启用浏览支持，则报错退出。
	// 实际使用时可以通过构建标签或运行时检测来替代。
	// 这里简单返回错误信息。
	fmt.Fprintf(os.Stderr, "ninja: browse tool not supported on this platform\n")
	return 1
}

// ToolMSVC MSVC 辅助工具（仅 Windows）。
func (n *NinjaMain) ToolMSVC(options *Options, args []string) int {
	if runtime.GOOS != "windows" {
		fmt.Fprintf(os.Stderr, "ninja: msvc tool only available on Windows\n")
		return 1
	}
	// 调用实际的 MSVCHelperMain 实现。
	// 注意：需要将当前进程的命令行参数传递给 MSVCHelperMain。
	// 由于 Go 没有直接等价物，这里假设 MSVCHelperMain 是一个外部函数，
	// 接收完整的命令行参数切片并返回退出码。
	// 实际使用时可能需要重新构造参数列表。
	return MSVCHelperMain(append([]string{"msvc"}, args...))
}

// ToolDeps 显示依赖日志中的依赖关系。
func (n *NinjaMain) ToolDeps(options *Options, args []string) int {
	var nodes []*builder.Node
	if len(args) == 0 {
		// 遍历 deps log 中的所有节点，只保留存活的条目
		for _, node := range n.deps_log_.Nodes() {
			if builder.IsDepsEntryLiveFor(node) {
				nodes = append(nodes, node)
			}
		}
	} else {
		collected, err := n.CollectTargetsFromArgs(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
			return 1
		}
		nodes = collected
	}

	disk := &builder.RealFileSystem{}
	for _, node := range nodes {
		deps := n.deps_log_.GetDeps(node)
		if deps == nil {
			fmt.Printf("%s: deps not found\n", node.Path)
			continue
		}

		mtime, err := disk.Stat(node.Path)
		if err != nil {
			// 记录错误但继续（与 C++ 中忽略 Stat 错误一致）
			fmt.Fprintf(os.Stderr, "ninja: warning: stat %s: %v\n", node.Path, err)
		}
		status := "VALID"
		if mtime.IsZero() || mtime.ModTime().UnixNano() > deps.Mtime {
			status = "STALE"
		}
		fmt.Printf("%s: #deps %d, deps mtime %d (%s)\n",
			node.Path, len(deps.Nodes), deps.Mtime, status)
		for _, dep := range deps.Nodes {
			fmt.Printf("    %s\n", dep.Path)
		}
		fmt.Println()
	}
	return 0
}

// ToolMissingDeps 检查依赖日志中是否存在缺失的生成文件依赖。
func (n *NinjaMain) ToolMissingDeps(options *Options, args []string) int {
	nodes, err := n.CollectTargetsFromArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}

	disk := disk.NewRealDiskInterface()
	printer := missingdeps.NewPrinter()
	scanner := missingdeps.NewScanner(printer, n.depsLog, n.state_, disk)

	for _, node := range nodes {
		scanner.ProcessNode(node)
	}
	scanner.PrintStats()
	if scanner.HadMissingDeps() {
		return 3
	}
	return 0
}

// ToolTargets 列出目标（按规则、深度或全部）。
func (n *NinjaMain) ToolTargets(options *Options, args []string) int {
	depth := 1
	if len(args) >= 1 {
		mode := args[0]
		switch mode {
		case "rule":
			ruleName := ""
			if len(args) > 1 {
				ruleName = args[1]
			}
			if ruleName == "" {
				ToolTargetsSourceList(n.state_)
			} else {
				ToolTargetsListByRule(n.state_, ruleName)
			}
			return 0
		case "depth":
			if len(args) > 1 {
				if d, err := strconv.Atoi(args[1]); err == nil {
					depth = d
				}
			}
		case "all":
			ToolTargetsListAll(n.state_)
			return 0
		default:
			suggestion := util.SpellcheckString(mode, "rule", "depth", "all")
			if suggestion != "" {
				fmt.Fprintf(os.Stderr, "ninja: unknown target tool mode '%s', did you mean '%s'?\n", mode, suggestion)
			} else {
				fmt.Fprintf(os.Stderr, "ninja: unknown target tool mode '%s'\n", mode)
			}
			return 1
		}
	}

	rootNodes, err := n.state_.RootNodes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}
	ToolTargetsList(rootNodes, depth, 0)
	return 0
}

// ToolRules 列出所有规则，可选打印描述。
func (n *NinjaMain) ToolRules(options *Options, args []string) int {
	// 解析选项（简化：手动解析 args 中的 -d）
	printDescription := false
	newArgs := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "-d" {
			printDescription = true
		} else if args[i] == "-h" {
			fmt.Println(`usage: ninja -t rules [options]

options:
  -d     also print the description of the rule
  -h     print this message`)
			return 1
		} else {
			newArgs = append(newArgs, args[i])
		}
	}
	// 忽略位置参数（通常没有）

	rules := n.state_.Bindings.GetRules()
	for name, rule := range rules {
		fmt.Print(name)
		if printDescription {
			if desc := rule.GetBinding("description"); desc != nil {
				fmt.Printf(": %s", desc.Unparse())
			}
		}
		fmt.Println()
	}
	return 0
}

// ToolWinCodePage 打印 Windows 代码页信息（仅 Windows）。
func (n *NinjaMain) ToolWinCodePage(options *Options, args []string) int {
	if len(args) != 0 {
		fmt.Println("usage: ninja -t wincodepage")
		return 1
	}
	// 获取当前 Windows 代码页（ANSI 或 UTF-8）
	// 注：Go 的 runtime 未直接提供 GetACP，但可以检查环境或使用默认假定。
	// 简单实现：判断是否处于 UTF-8 环境（如代码页 65001）
	// 这里输出固定信息，实际需要调用 Windows API。
	fmt.Println("Build file encoding: ANSI") // 或检测是否为 UTF-8
	return 0
}

// PrintCommandMode 打印命令的模式
type PrintCommandMode int

const (
	PCM_Single PrintCommandMode = iota
	PCM_All
)

// PrintCommands 递归打印边（及其依赖）的命令行。
func PrintCommands(edge *builder.Edge, seen map[*builder.Edge]bool, mode PrintCommandMode) {
	if edge == nil {
		return
	}
	if seen[edge] {
		return
	}
	seen[edge] = true

	if mode == PCM_All {
		for _, in := range edge.Inputs {
			PrintCommands(in.InEdge, seen, mode)
		}
	}

	if !edge.IsPhony() {
		fmt.Println(edge.EvaluateCommand(false))
	}
}

// ToolCommands 列出构建给定目标所需的所有命令。
func (n *NinjaMain) ToolCommands(options *Options, args []string) int {
	// 解析选项
	mode := PCM_All
	var newArgs []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-s" {
			mode = PCM_Single
		} else if args[i] == "-h" {
			fmt.Println(`usage: ninja -t commands [options] [targets]

options:
  -s     only print the final command to build [target], not the whole chain`)
			return 1
		} else {
			newArgs = append(newArgs, args[i])
		}
	}

	nodes, err := n.CollectTargetsFromArgs(newArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}

	seen := make(map[*builder.Edge]bool)
	for _, node := range nodes {
		PrintCommands(node.InEdge, seen, mode)
	}
	return 0
}

// ToolInputs 列出给定目标所需的所有输入文件。
func (n *NinjaMain) ToolInputs(options *Options, args []string) int {
	// 解析选项
	print0 := false
	shellEscape := true
	dependencyOrder := false

	var newArgs []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-0", "--print0":
			print0 = true
		case "-E", "--no-shell-escape":
			shellEscape = false
		case "-d", "--dependency-order":
			dependencyOrder = true
		case "-h", "--help":
			fmt.Printf(`Usage: ninja -t inputs [options] [targets]

List all inputs used for a set of targets, sorted in dependency order.
Note that by default, results are shell escaped, and sorted alphabetically,
and never include validation target paths.

Options:
  -h, --help          Print this message.
  -0, --print0        Use \0, instead of \n as a line terminator.
  -E, --no-shell-escape   Do not shell escape the result.
  -d, --dependency-order  Sort results by dependency order.
`)
			return 1
		default:
			newArgs = append(newArgs, args[i])
		}
	}

	nodes, err := n.CollectTargetsFromArgs(newArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}

	collector := NewInputsCollector()
	for _, node := range nodes {
		collector.VisitNode(node)
	}

	inputs := collector.GetInputsAsStrings(shellEscape)
	if !dependencyOrder {
		sort.Strings(inputs)
	}

	if print0 {
		for _, input := range inputs {
			fmt.Print(input)
			fmt.Print("\x00")
		}
	} else {
		for _, input := range inputs {
			fmt.Println(input)
		}
	}
	return 0
}

// ToolMultiInputs 为每个目标输出一行，包含目标名、分隔符和输入名。
func (n *NinjaMain) ToolMultiInputs(options *Options, args []string) int {
	// 解析选项
	terminator := byte('\n')
	delimiter := "\t"

	var newArgs []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-0", "--print0":
			terminator = 0
		case "-d", "--delimiter":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "ninja: missing argument for %s\n", args[i])
				return 1
			}
			delimiter = args[i+1]
			i++
		case "-h", "--help":
			fmt.Printf(`Usage: ninja -t multi-inputs [options] [targets]

Print one or more sets of inputs required to build targets, sorted in dependency order.
The tool works like inputs tool but with addition of the target for each line.
The output will be a series of lines with the following elements:
<target> <delimiter> <input> <terminator>
Note that a given input may appear for several targets if it is used by more than one targets.

Options:
  -h, --help                   Print this message.
  -d, --delimiter=DELIM        Use DELIM instead of TAB for field delimiter.
  -0, --print0                 Use \0, instead of \n as a line terminator.
`)
			return 1
		default:
			newArgs = append(newArgs, args[i])
		}
	}

	nodes, err := n.CollectTargetsFromArgs(newArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}

	for _, node := range nodes {
		collector := NewInputsCollector()
		collector.VisitNode(node)
		inputs := collector.GetInputsAsStrings(false) // 不转义，保持原始路径
		for _, input := range inputs {
			fmt.Printf("%s%s%s", node.Path, delimiter, input)
			if terminator == 0 {
				fmt.Printf("%c", terminator)
			} else {
				fmt.Println()
			}
		}
	}
	return 0
}

// ToolClean 清理构建产物。
func (n *NinjaMain) ToolClean(options *Options, args []string) int {
	// 解析选项
	generator := false
	cleanRules := false
	var targets []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-g":
			generator = true
		case "-r":
			cleanRules = true
		case "-h":
			fmt.Println(`usage: ninja -t clean [options] [targets]

options:
  -g     also clean files marked as ninja generator output
  -r     interpret targets as a list of rules to clean instead`)
			return 1
		default:
			targets = append(targets, args[i])
		}
	}

	if cleanRules && len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "ninja: expected a rule to clean")
		return 1
	}

	cleaner := NewCleaner(n.state_, n.config_, n.disk_interface_)
	if len(targets) > 0 {
		if cleanRules {
			return cleaner.CleanRules(targets)
		}
		return cleaner.CleanTargets(targets)
	}
	return cleaner.CleanAll(generator)
}

// ToolCleanDead 清理不再由当前 manifest 生成的旧输出。
func (n *NinjaMain) ToolCleanDead(options *Options, args []string) int {
	cleaner := NewCleaner(n.state_, n.config_, n.disk_interface_)
	return cleaner.CleanDead(n.build_log_.Entries())
}

// EvaluateCommandMode 命令求值模式
type EvaluateCommandMode int

const (
	ECM_NORMAL EvaluateCommandMode = iota
	ECM_EXPAND_RSPFILE
)

// EvaluateCommandWithRspfile 返回边的命令，可选展开响应文件内容。
func EvaluateCommandWithRspfile(edge *builder.Edge, mode EvaluateCommandMode) string {
	command := edge.EvaluateCommand(false) // 不含 rspfile 内容
	if mode == ECM_NORMAL {
		return command
	}

	rspfile := edge.GetUnescapedRspfile()
	if rspfile == "" {
		return command
	}

	// 查找 rspfile 在命令中的位置（需考虑 @rspfile、-f rspfile、--option-file=rspfile 三种模式）
	idx := strings.Index(command, rspfile)
	if idx == -1 || idx == 0 {
		return command
	}
	// 检查前一个字符
	prevChar := command[idx-1]
	if prevChar != '@' && !strings.Contains(command[idx-14:idx], "--option-file=") && !strings.Contains(command[idx-3:idx], "-f ") {
		return command
	}

	rspContent := edge.GetBinding("rspfile_content")
	// 将换行符替换为空格（响应文件内容中换行表示参数分隔）
	rspContent = strings.ReplaceAll(rspContent, "\n", " ")

	switch {
	case prevChar == '@':
		// @rspfile 形式
		return command[:idx-1] + rspContent + command[idx+len(rspfile):]
	case strings.Contains(command[idx-14:idx], "--option-file="):
		// --option-file=rspfile 形式
		return command[:idx-14] + rspContent + command[idx+len(rspfile):]
	case strings.Contains(command[idx-3:idx], "-f "):
		// -f rspfile 形式
		return command[:idx-3] + rspContent + command[idx+len(rspfile):]
	default:
		return command
	}
}

// PrintCompdbObjectsForEdge 输出一个边对应的编译数据库条目（JSON 格式）。
func PrintCompdbObjectsForEdge(directory string, edge *builder.Edge, evalMode EvaluateCommandMode) {
	command := EvaluateCommandWithRspfile(edge, evalMode)
	first := true

	for _, input := range edge.Inputs {
		if !first {
			fmt.Print(",")
		}
		fmt.Printf("\n  {\n    \"directory\": \"")
		PrintJSONString(directory)
		fmt.Printf("\",\n    \"command\": \"")
		PrintJSONString(command)
		fmt.Printf("\",\n    \"file\": \"")
		PrintJSONString(input.Path)
		fmt.Printf("\",\n    \"output\": \"")
		PrintJSONString(edge.Outputs[0].Path)
		fmt.Printf("\"\n  }")
		first = false
	}
}

// ToolCompilationDatabase 生成 JSON 编译数据库。
func (n *NinjaMain) ToolCompilationDatabase(options *Options, args []string) int {
	// 解析选项
	evalMode := ECM_NORMAL
	var rules []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-x":
			evalMode = ECM_EXPAND_RSPFILE
		case "-h":
			fmt.Println(`usage: ninja -t compdb [options] [rules]

options:
  -x     expand @rspfile style response file invocations`)
			return 1
		default:
			rules = append(rules, args[i])
		}
	}

	directory, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ninja: failed to get working directory: %v\n", err)
		return 1
	}

	fmt.Print("[")
	first := true
	for _, edge := range n.state_.Edges {
		if len(edge.Inputs) == 0 {
			continue
		}
		if len(rules) == 0 {
			if !first {
				fmt.Print(",")
			}
			PrintCompdbObjectsForEdge(directory, edge, evalMode)
			first = false
		} else {
			for _, ruleName := range rules {
				if edge.Rule.Name == ruleName {
					if !first {
						fmt.Print(",")
					}
					PrintCompdbObjectsForEdge(directory, edge, evalMode)
					first = false
					break
				}
			}
		}
	}
	fmt.Println("\n]")
	return 0
}

// ToolRecompact 重新压缩内部日志文件（ninja_log 和 ninja_deps）。
func (n *NinjaMain) ToolRecompact(options *Options, args []string) int {
	if !n.EnsureBuildDirExists() {
		return 1
	}
	if !n.OpenBuildLog(true) || !n.OpenDepsLog(true) {
		return 1
	}
	return 0
}

// ToolRestat 重新统计构建日志中指定输出文件的 mtime。
func (n *NinjaMain) ToolRestat(options *Options, args []string) int {
	// 解析选项
	buildDir := n.build_dir_ // 使用结构体已有的 build_dir 字段
	var outputs []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--builddir" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "ninja: --builddir requires an argument")
				return 1
			}
			buildDir = args[i+1]
			i++
		} else if args[i] == "-h" || args[i] == "--help" {
			fmt.Println("usage: ninja -t restat [--builddir=DIR] [outputs]")
			return 1
		} else {
			outputs = append(outputs, args[i])
		}
	}

	logPath := ".ninja_log"
	if buildDir != "" {
		logPath = buildDir + "/" + logPath
	}

	// 加载构建日志
	if err := n.build_log_.Load(logPath); err != nil {
		// 如果文件不存在，忽略
		if os.IsNotExist(err) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "ninja: loading build log %s: %v\n", logPath, err)
		return 1
	}

	// 调用 Restat 更新记录
	if err := n.build_log_.Restat(logPath, n.disk_interface_, outputs); err != nil {
		fmt.Fprintf(os.Stderr, "ninja: failed restat: %v\n", err)
		return 1
	}

	// 如果不是 dry run，重新打开日志文件用于写入
	if !n.config_.DryRun {
		if err := n.build_log_.OpenForWrite(logPath, n); err != nil {
			fmt.Fprintf(os.Stderr, "ninja: opening build log: %v\n", err)
			return 1
		}
	}

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
