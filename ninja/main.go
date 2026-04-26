package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	// 全局调试标志
	g_metrics                *Metrics
	g_explaining             bool
	g_keep_depfile           bool = false
	g_keep_rsp               bool = false
	g_experimental_statcache      = true
)

const kNinjaVersion = "1.14.0.git"

type NinjaMain struct {
	BuildLogUser
	/// Command line used to run Ninja.
	ninja_command_ string

	/// Build configuration set from flags (e.g. parallelism).
	config_ *BuildConfig

	/// Loaded state (rules, nodes).
	state_ *State

	/// Functions for accessing the disk.
	disk_interface_ FileSystem

	/// The build directory, used for storing the build log etc.
	build_dir_ string

	build_log_         *BuildLog
	deps_log_          *DepsLog
	start_time_millis_ int64
}

// RebuildManifest 如果必要则重新构建清单文件。
// 返回 true 表示清单已被重建。
func (n *NinjaMain) RebuildManifest(inputFile string, err *string, status Status) bool {
	path := inputFile
	if path == "" {
		*err = "empty path"
		return false
	}
	var slash_bits uint64
	CanonicalizePathString(&path, &slash_bits)
	node := n.state_.LookupNode(path)
	if node == nil {
		return false
	}

	bd := NewBuilder(n.state_, n.config_, n.build_log_, n.deps_log_, n.start_time_millis_, n.disk_interface_, status)
	if !bd.AddTarget(node, err) {
		return false
	}

	if bd.AlreadyUpToDate() {
		return false
	}

	if bd.Build(err) != ExitSuccess {
		return false
	}

	// 只有当节点现在变脏时才认为清单被重建（可能被 restat 清理）
	if !node.Dirty() {
		// 重置状态以避免问题（如 https://github.com/ninja-build/ninja/issues/874）
		n.state_.Reset()
		return false
	}

	return true
}

// ParsePreviousElapsedTimes 从构建日志中加载每条边上次构建的耗时，用于 ETA 预测。
func (n *NinjaMain) ParsePreviousElapsedTimes() {
	for _, edge := range n.state_.Edges {
		for _, out := range edge.outputs_ {
			logEntry := n.build_log_.LookupByOutput(out.path_)
			if logEntry == nil {
				continue // 可能该边的其他输出有记录，继续检查下一个输出
			}
			edge.prev_elapsed_time_millis = int64(logEntry.EndTime - logEntry.StartTime)
			break // 只要找到一个输出有记录即可，继续下一条边
		}
	}
}

// CollectTarget 将命令行路径转换为 Node，支持特殊语法 "foo.cc^"（取第一个输出）。
func (n *NinjaMain) CollectTarget(cpath string, err *string) *Node {
	path := cpath
	if path == "" {
		*err = "empty path"
		return nil
	}
	var slashBits uint64
	CanonicalizePathString(&path, &slashBits)

	// 特殊语法：以 '^' 结尾表示取该节点的第一个输出
	firstDependent := false
	if len(path) > 0 && path[len(path)-1] == '^' {
		path = path[:len(path)-1]
		firstDependent = true
	}

	node := n.state_.LookupNode(path)
	if node != nil {
		if firstDependent {
			if len(node.out_edges_) == 0 {
				// 没有出边，尝试从 deps log 中查找反向依赖
				revNode := n.deps_log_.GetFirstReverseDepsNode(node)
				if revNode == nil {
					*err = "'" + path + "' has no out edge"
					return nil
				}
				node = revNode
			} else {
				edge := node.out_edges_[0]
				if len(edge.outputs_) == 0 {
					// 不应发生，防御性代码
					panic("edge has no outputs")
					return nil
				}
				node = edge.outputs_[0]
			}
		}
		return node
	}

	// 节点不存在，构建错误信息
	decanon := PathDecanonicalized(path, slashBits)
	*err = fmt.Sprintf("unknown target '%s'", decanon)
	if path == "clean" {
		*err += ", did you mean 'ninja -t clean'?"
	} else if path == "help" {
		*err += ", did you mean 'ninja -h'?"
	} else {
		suggestion := n.state_.SpellcheckNode(path)
		if suggestion != nil {
			*err += fmt.Sprintf(", did you mean '%s'?", suggestion.path_)
		}
	}
	return nil
}

// CollectTargetsFromArgs 将命令行参数转换为 Node 列表，如果无参数则使用默认目标。
func (n *NinjaMain) CollectTargetsFromArgs(args []string, targets *[]*Node, err *string) bool {
	if len(args) == 0 {
		*targets = n.state_.DefaultNodes(err)
		return *err == ""
	}

	for _, arg := range args {
		node := n.CollectTarget(arg, err)
		if *err != "" {
			return false
		}
		*targets = append(*targets, node)
	}
	return true
}

// ToolGraph 输出 graphviz dot 文件（用于可视化依赖图）。
func (n *NinjaMain) ToolGraph(options *Options, args []string) int {
	var err string
	var nodes []*Node
	if !n.CollectTargetsFromArgs(args, &nodes, &err) {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}

	graph := NewGraphViz(n.state_, n.disk_interface_)
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

	dyndepLoader := NewDyndepLoader(n.state_, n.disk_interface_)

	for _, arg := range args {
		var err string
		node := n.CollectTarget(arg, &err)
		if err != "" {
			fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
			return 1
		}

		fmt.Printf("%s:\n", node.path_)
		if edge := node.in_edge(); edge != nil {
			// 如果边有挂起的 dyndep 文件，尝试加载
			if edge.dyndep_ != nil && edge.dyndep_.dyndep_pending_ {
				if !dyndepLoader.LoadDyndeps(edge.dyndep_, &err) {
					fmt.Fprintf(os.Stderr, "ninja: warning: %v\n", err)
				}
			}
			fmt.Printf("  input: %s\n", edge.Rule.Name)
			for idx, in := range edge.inputs_ {
				label := ""
				if edge.IsImplicit(idx) {
					label = "| "
				} else if edge.IsOrderOnly(idx) {
					label = "|| "
				}
				fmt.Printf("    %s%s\n", label, in.path_)
			}
			if len(edge.validations_) > 0 {
				fmt.Printf("  validations:\n")
				for _, v := range edge.validations_ {
					fmt.Printf("    %s\n", v.path_)
				}
			}
		}
		fmt.Printf("  outputs:\n")
		for _, outEdge := range node.out_edges_ {
			for _, out := range outEdge.outputs_ {
				fmt.Printf("    %s\n", out.path_)
			}
		}
		if validationEdges := node.validation_out_edges_; len(validationEdges) > 0 {
			fmt.Printf("  validation for:\n")
			for _, outEdge := range validationEdges {
				for _, out := range outEdge.outputs_ {
					fmt.Printf("    %s\n", out.path_)
				}
			}
		}
	}
	return 0
}

// ToolTargetsList 递归打印节点树（深度限制）。
func ToolTargetsList(nodes []*Node, depth, indent int) {
	for _, n := range nodes {
		for i := 0; i < indent; i++ {
			fmt.Print("  ")
		}
		if edge := n.in_edge(); edge != nil {
			fmt.Printf("%s: %s\n", n.path_, edge.Rule.Name)
			if depth > 1 || depth <= 0 {
				ToolTargetsList(edge.inputs_, depth-1, indent+1)
			}
		} else {
			fmt.Printf("%s\n", n.path_)
		}
	}
}

// ToolTargetsSourceList 打印所有源文件（没有入边的节点）。
func ToolTargetsSourceList(state *State) {
	for _, edge := range state.Edges {
		for _, in := range edge.inputs_ {
			if in.in_edge() == nil {
				fmt.Println(in.path_)
			}
		}
	}
}

// ToolTargetsListByRule 打印指定规则生成的所有输出。
func ToolTargetsListByRule(state *State, ruleName string) {
	outputs := make(map[string]bool)
	for _, edge := range state.Edges {
		if edge.Rule.Name == ruleName {
			for _, out := range edge.outputs_ {
				outputs[out.path_] = true
			}
		}
	}
	for out := range outputs {
		fmt.Println(out)
	}
}

// ToolTargetsListAll 打印所有输出及其所属规则。
func ToolTargetsListAll(state *State) {
	for _, edge := range state.Edges {
		for _, out := range edge.outputs_ {
			fmt.Printf("%s: %s\n", out.path_, edge.Rule.Name)
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
	var nodes []*Node
	if len(args) == 0 {
		// 遍历 deps log 中的所有节点，只保留存活的条目
		for _, node := range n.deps_log_.Nodes() {
			if IsDepsEntryLiveFor(node) {
				nodes = append(nodes, node)
			}
		}
	} else {
		var err string
		if !n.CollectTargetsFromArgs(args, &nodes, &err) {
			fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
			return 1
		}
	}

	disk := &RealFileSystem{}
	for _, node := range nodes {
		deps := n.deps_log_.GetDeps(node)
		if deps == nil {
			fmt.Printf("%s: deps not found\n", node.path_)
			continue
		}
		var err string
		mtime := disk.Stat(node.path_, &err)
		if err != "" {
			// 记录错误但继续（与 C++ 中忽略 Stat 错误一致）
			fmt.Fprintf(os.Stderr, "ninja: warning: stat %s: %v\n", node.path_, err)
		}
		status := "VALID"
		if mtime == 0 || mtime > deps.GetMtime() {
			status = "STALE"
		}
		fmt.Printf("%s: #deps %d, deps mtime %d (%s)\n",
			node.path_, deps.GetNodeCount(), deps.GetMtime(), status)
		for _, dep := range deps.GetNodes() {
			fmt.Printf("    %s\n", dep.path_)
		}
		fmt.Println()
	}
	return 0
}

// ToolMissingDeps 检查依赖日志中是否存在缺失的生成文件依赖。
func (n *NinjaMain) ToolMissingDeps(options *Options, args []string) int {
	var err string
	var nodes []*Node

	if !n.CollectTargetsFromArgs(args, &nodes, &err) {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}

	disk := NewRealFileSystem()
	printer := NewPrinter()
	scanner := NewScanner(printer, n.deps_log_, n.state_, disk)

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
			suggestion := SpellcheckString(mode, []string{"rule", "depth", "all"})
			if suggestion != "" {
				fmt.Fprintf(os.Stderr, "ninja: unknown target tool mode '%s', did you mean '%s'?\n", mode, suggestion)
			} else {
				fmt.Fprintf(os.Stderr, "ninja: unknown target tool mode '%s'\n", mode)
			}
			return 1
		}
	}
	var err string
	rootNodes := n.state_.RootNodes(&err)
	if err != "" {
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
func PrintCommands(edge *Edge, seen map[*Edge]bool, mode PrintCommandMode) {
	if edge == nil {
		return
	}
	if seen[edge] {
		return
	}
	seen[edge] = true

	if mode == PCM_All {
		for _, in := range edge.inputs_ {
			PrintCommands(in.in_edge(), seen, mode)
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
	var nodes []*Node
	var err string
	if !n.CollectTargetsFromArgs(newArgs, &nodes, &err) {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}

	seen := make(map[*Edge]bool)
	for _, node := range nodes {
		PrintCommands(node.in_edge(), seen, mode)
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

	var nodes []*Node
	var err string
	if !n.CollectTargetsFromArgs(newArgs, &nodes, &err) {
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

	var nodes []*Node
	var err string
	if !n.CollectTargetsFromArgs(newArgs, &nodes, &err) {
		fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
		return 1
	}

	for _, node := range nodes {
		collector := NewInputsCollector()
		collector.VisitNode(node)
		inputs := collector.GetInputsAsStrings(false) // 不转义，保持原始路径
		for _, input := range inputs {
			fmt.Printf("%s%s%s", node.path_, delimiter, input)
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
func EvaluateCommandWithRspfile(edge *Edge, mode EvaluateCommandMode) string {
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
func PrintCompdbObjectsForEdge(directory string, edge *Edge, evalMode EvaluateCommandMode) {
	command := EvaluateCommandWithRspfile(edge, evalMode)
	first := true

	for _, input := range edge.inputs_ {
		if !first {
			fmt.Print(",")
		}
		fmt.Printf("\n  {\n    \"directory\": \"")
		PrintJSONString(directory)
		fmt.Printf("\",\n    \"command\": \"")
		PrintJSONString(command)
		fmt.Printf("\",\n    \"file\": \"")
		PrintJSONString(input.path_)
		fmt.Printf("\",\n    \"output\": \"")
		PrintJSONString(edge.outputs_[0].path_)
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
		if len(edge.inputs_) == 0 {
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

// CompdbTargetsAction 动作类型
type CompdbTargetsAction int

const (
	ActionDisplayHelpAndExit CompdbTargetsAction = iota
	ActionEmitCommands
)

// CompdbTargets 解析 compdb-targets 子工具的参数
type CompdbTargets struct {
	Action   CompdbTargetsAction
	EvalMode EvaluateCommandMode
	Targets  []string
}

// CreateFromArgs 从命令行参数创建 CompdbTargets 实例
func CreateCompdbTargetsFromArgs(args []string) *CompdbTargets {
	// 模拟 getopt 解析 -h 和 -x
	ret := &CompdbTargets{
		Action:   ActionEmitCommands,
		EvalMode: ECM_NORMAL,
	}
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-x":
			ret.EvalMode = ECM_EXPAND_RSPFILE
		case "-h":
			ret.Action = ActionDisplayHelpAndExit
			return ret
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "compdb-targets expects the name of at least one target")
		ret.Action = ActionDisplayHelpAndExit
	} else {
		ret.Targets = positional
	}
	return ret
}

// PrintCompdb 输出 JSON 编译数据库（用于 compdb-targets）
func PrintCompdb(directory string, edges []*Edge, evalMode EvaluateCommandMode) {
	fmt.Print("[")
	first := true
	for _, edge := range edges {
		if edge.IsPhony() || len(edge.inputs_) == 0 {
			continue
		}
		if !first {
			fmt.Print(",")
		}
		PrintCompdbObjectsForEdge(directory, edge, evalMode)
		first = false
	}
	fmt.Println("\n]")
}

// ToolCompilationDatabaseForTargets 为指定目标生成编译数据库。
func (n *NinjaMain) ToolCompilationDatabaseForTargets(options *Options, args []string) int {
	compdb := CreateCompdbTargetsFromArgs(args)

	switch compdb.Action {
	case ActionDisplayHelpAndExit:
		fmt.Println(`usage: ninja -t compdb [-hx] target [targets]

options:
  -h     display this help message
  -x     expand @rspfile style response file invocations`)
		return 1

	case ActionEmitCommands:
		collector := NewCommandCollector()
		for _, targetArg := range compdb.Targets {
			var err string
			node := n.CollectTarget(targetArg, &err)
			if err != "" {
				fmt.Fprintf(os.Stderr, "ninja: %v\n", err)
				return 1
			}
			if node.in_edge() == nil {
				fmt.Fprintf(os.Stderr, "ninja: '%s' is not a target (i.e. it is not an output of any `build` statement)\n", node.path_)
				return 1
			}
			collector.CollectFrom(node)
		}

		directory, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ninja: failed to get working directory: %v\n", err)
			return 1
		}
		PrintCompdb(directory, collector.InEdges(), compdb.EvalMode)
	}
	return 0
}

// ToolUrtle 打印乌龟图案（彩蛋）。
func (n *NinjaMain) ToolUrtle(options *Options, args []string) int {
	urtle := "xx"
	count := 0
	for _, ch := range urtle {
		if ch >= '0' && ch <= '9' {
			count = count*10 + int(ch-'0')
		} else {
			for i := 0; i < max(count, 1); i++ {
				fmt.Printf("%c", ch)
			}
			count = 0
		}
	}
	return 0
}

const EXIT_SUCCESS = 0
const EXIT_FAILURE = 1

// ToolRestat 重新统计构建日志中指定输出文件的 mtime。
func (n *NinjaMain) ToolRestat(options *Options, args []string) int {
	// 解析选项
	buildDir := n.build_dir_ // 使用结构体已有的 build_dir 字段
	var outputs []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--builddir" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "ninja: --builddir requires an argument")
				return EXIT_FAILURE
			}
			buildDir = args[i+1]
			i++
		} else if args[i] == "-h" || args[i] == "--help" {
			fmt.Println("usage: ninja -t restat [--builddir=DIR] [outputs]")
			return EXIT_FAILURE
		} else {
			outputs = append(outputs, args[i])
		}
	}

	logPath := ".ninja_log"
	if buildDir != "" {
		logPath = buildDir + "/" + logPath
	}

	// 加载构建日志
	var err string
	status := n.build_log_.Load(logPath, &err)
	if status == LOAD_ERROR {
		// 如果文件不存在，忽略
		fmt.Fprintf(os.Stderr, "ninja: loading build log %s: %v\n", logPath, err)
		return EXIT_FAILURE
	}
	if status == LOAD_NOT_FOUND {
		// Nothing to restat, ignore this
		return EXIT_SUCCESS
	}
	if err != "" {
		// Hack: Load() can return a warning via err by returning LOAD_SUCCESS.
		fmt.Printf("%s", err)
		err = ""
	}

	// 调用 Restat 更新记录
	success := n.build_log_.Restat(logPath, n.disk_interface_, outputs, &err)
	if !success {
		fmt.Fprintf(os.Stderr, "ninja: failed restat: %v\n", err)
		return EXIT_FAILURE
	}

	// 如果不是 dry run，重新打开日志文件用于写入
	if !n.config_.GetDryRun() {
		if !n.build_log_.OpenForWrite(logPath, n, &err) {
			fmt.Fprintf(os.Stderr, "ninja: opening build log: %v\n", err)
			return EXIT_FAILURE
		}
	}

	return EXIT_SUCCESS
}

type When int

const (
	RUN_AFTER_FLAGS When = iota
	RUN_AFTER_LOAD
	RUN_AFTER_LOGS
)

// Tool 定义子工具
type Tool struct {
	name string
	desc string
	when When
	f    func(*NinjaMain, *Options, []string) int
}

// 工具列表（对应 C++ 的 kTools）
var kTools = []Tool{
	{"browse", "browse dependency graph in a web browser", RUN_AFTER_LOAD, (*NinjaMain).ToolBrowse},
	{"clean", "clean built files", RUN_AFTER_LOAD, (*NinjaMain).ToolClean},
	{"commands", "list all commands required to rebuild given targets", RUN_AFTER_LOAD, (*NinjaMain).ToolCommands},
	{"inputs", "list all inputs required to rebuild given targets", RUN_AFTER_LOAD, (*NinjaMain).ToolInputs},
	{"multi-inputs", "print one or more sets of inputs required to build targets", RUN_AFTER_LOAD, (*NinjaMain).ToolMultiInputs},
	{"deps", "show dependencies stored in the deps log", RUN_AFTER_LOGS, (*NinjaMain).ToolDeps},
	{"missingdeps", "check deps log dependencies on generated files", RUN_AFTER_LOGS, (*NinjaMain).ToolMissingDeps},
	{"graph", "output graphviz dot file for targets", RUN_AFTER_LOAD, (*NinjaMain).ToolGraph},
	{"query", "show inputs/outputs for a path", RUN_AFTER_LOGS, (*NinjaMain).ToolQuery},
	{"targets", "list targets by their rule or depth in the DAG", RUN_AFTER_LOAD, (*NinjaMain).ToolTargets},
	{"compdb", "dump JSON compilation database to stdout", RUN_AFTER_LOAD, (*NinjaMain).ToolCompilationDatabase},
	{"compdb-targets", "dump JSON compilation database for a given list of targets to stdout", RUN_AFTER_LOAD, (*NinjaMain).ToolCompilationDatabaseForTargets},
	{"recompact", "recompacts ninja-internal data structures", RUN_AFTER_LOAD, (*NinjaMain).ToolRecompact},
	{"restat", "restats all outputs in the build log", RUN_AFTER_FLAGS, (*NinjaMain).ToolRestat},
	{"rules", "list all rules", RUN_AFTER_LOAD, (*NinjaMain).ToolRules},
	{"cleandead", "clean built files that are no longer produced by the manifest", RUN_AFTER_LOGS, (*NinjaMain).ToolCleanDead},
	{"urtle", "", RUN_AFTER_FLAGS, (*NinjaMain).ToolUrtle},
}

// ChooseTool 查找工具，如果工具名为 "list" 则打印列表并返回 nil，
// 如果找不到则报错并退出程序。
func ChooseTool(toolName string) *Tool {
	if toolName == "list" {
		fmt.Println("ninja subtools:")
		for _, tool := range kTools {
			if tool.desc != "" {
				fmt.Printf("%11s  %s\n", tool.name, tool.desc)
			}
		}
		return nil
	}
	for _, tool := range kTools {
		if tool.name == toolName {
			return &tool
		}
	}
	// 拼写检查（简化）
	fmt.Fprintf(os.Stderr, "ninja: unknown tool '%s'\n", toolName)
	os.Exit(1)
	return nil
}

// debugEnable 启用调试模式。返回 false 表示 Ninja 应退出，true 表示继续。
func debugEnable(name string) bool {
	switch name {
	case "list":
		fmt.Println(`debugging modes:
  stats        print operation counts/timing info
  explain      explain what caused a command to execute
  keepdepfile  don't delete depfiles after they're read by ninja
  keeprsp      don't delete @response files on success
  nostatcache  don't batch stat() calls per directory and cache them
multiple modes can be enabled via -d FOO -d BAR`)
		return false
	case "stats":
		g_metrics = &Metrics{}
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
		suggestion := SpellcheckString(name, []string{"stats", "explain", "keepdepfile", "keeprsp", "nostatcache"})
		if suggestion != "" {
			fmt.Fprintf(os.Stderr, "ninja: unknown debug setting '%s', did you mean '%s'?\n", name, suggestion)
		} else {
			fmt.Fprintf(os.Stderr, "ninja: unknown debug setting '%s'\n", name)
		}
		return false
	}
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

// warningEnable 设置警告标志。返回 false 表示 Ninja 应退出，true 表示继续。
func warningEnable(name string, options *Options) bool {
	switch name {
	case "list":
		fmt.Println(`warning flags:
  phonycycle={err,warn}  phony build statement references itself`)
		return false
	case "phonycycle=err":
		options.phony_cycle_should_err = true
		return true
	case "phonycycle=warn":
		options.phony_cycle_should_err = false
		return true
	case "dupbuild=err", "dupbuild=warn":
		fmt.Fprintf(os.Stderr, "ninja: warning: deprecated warning 'dupbuild'\n")
		return true
	case "depfilemulti=err", "depfilemulti=warn":
		fmt.Fprintf(os.Stderr, "ninja: warning: deprecated warning 'depfilemulti'\n")
		return true
	default:
		suggestion := SpellcheckString(name, []string{"phonycycle=err", "phonycycle=warn"})
		if suggestion != "" {
			fmt.Fprintf(os.Stderr, "ninja: unknown warning flag '%s', did you mean '%s'?\n", name, suggestion)
		} else {
			fmt.Fprintf(os.Stderr, "ninja: unknown warning flag '%s'\n", name)
		}
		return false
	}
}

// OpenBuildLog 加载并可选地重新压实构建日志，然后在非 dry-run 模式下打开写入。
func (n *NinjaMain) OpenBuildLog(recompactOnly bool) bool {
	log_path := ".ninja_log"
	if n.build_dir_ != "" {
		log_path = n.build_dir_ + "/" + log_path
	}

	// 加载日志
	var err string
	status := n.build_log_.Load(log_path, &err)
	if status == LOAD_ERROR {
		fmt.Printf("loading build log %s: %s", log_path, err)
		return false
	}
	if err != "" {
		// Hack: Load() can return a warning via err by returning LOAD_SUCCESS.
		fmt.Printf("%s", err)
		err = ""
	}

	if recompactOnly {
		if status == LOAD_NOT_FOUND {
			return true
		}
		success := n.build_log_.Recompact(log_path, n, &err)
		if !success {
			fmt.Fprintf(os.Stderr, "ninja: failed recompaction: %v\n", err)
		}
		return success
	}

	if !n.config_.GetDryRun() {
		if !n.build_log_.OpenForWrite(log_path, n, &err) {
			fmt.Fprintf(os.Stderr, "ninja: opening build log: %v\n", err)
			return false
		}
	}
	return true
}

// OpenDepsLog 加载并可选地重新压实依赖日志，然后在非 dry-run 模式下打开写入。
func (n *NinjaMain) OpenDepsLog(recompactOnly bool) bool {
	path := ".ninja_deps"
	if n.build_dir_ != "" {
		path = n.build_dir_ + "/" + path
	}

	// 加载日志
	var err string
	status := n.deps_log_.Load(path, n.state_, &err) // path,
	if status == LOAD_ERROR {
		fmt.Printf("loading deps log %s: %s", path, err)
		return false
	}
	if err != "" {
		// Hack: Load() can return a warning via err by returning LOAD_SUCCESS.
		fmt.Printf("%s", err)
		err = ""
	}
	// 忽略警告（假设已输出）

	if recompactOnly {
		if status == LOAD_NOT_FOUND {
			return true
		}
		success := n.deps_log_.Recompact(path, &err)
		if !success {
			fmt.Fprintf(os.Stderr, "ninja: failed recompaction: %v\n", err)
		}
		return success
	}

	if !n.config_.GetDryRun() {
		if !n.deps_log_.OpenForWrite(path, &err) {
			fmt.Fprintf(os.Stderr, "ninja: opening deps log: %v\n", err)
			return false
		}
	}
	return true
}

// DumpMetrics 打印性能指标和哈希表负载信息。
func (n *NinjaMain) DumpMetrics() {
	if g_metrics != nil {
		g_metrics.Report()
	}
	fmt.Println()
	// Go 的 map 没有 bucket_count 方法，仅输出条目数。
	fmt.Printf("path->node hash load %.2f (%d entries)\n",
		float64(len(n.state_.Paths)), len(n.state_.Paths))
}

// EnsureBuildDirExists 确保构建目录存在，若需要则创建它。
func (n *NinjaMain) EnsureBuildDirExists() bool {
	n.build_dir_ = n.state_.Bindings.LookupVariable("builddir")
	if n.build_dir_ != "" && !n.config_.GetDryRun() {
		// 创建目录（如果不存在）
		if err := os.MkdirAll(n.build_dir_, 0755); err != nil && !os.IsExist(err) {
			fmt.Fprintf(os.Stderr, "ninja: creating build directory %s: %v\n", n.build_dir_, err)
			return false
		}
	}
	return true
}

// SetupJobserverClient 根据 MAKEFLAGS 环境变量创建 jobserver 客户端。
// 返回客户端（可能为 nil）以及错误（如果存在）。
func (n *NinjaMain) SetupJobserverClient(status Status) JobserverClient {
	// 如果是 dry-run 或明确指定了并行数，则忽略 jobserver
	if n.config_.GetDisableJobserverClient() {
		return nil
	}

	makeflags := os.Getenv("MAKEFLAGS")
	if makeflags == "" {
		return nil
	}

	config, err := ParseNativeMakeFlagsValue(makeflags)
	if err != nil {
		if n.config_.GetVerbosity() > QUIET {
			status.Warning("Ignoring jobserver: %v [%s]", err, makeflags)
		}
		return nil
	}
	if config.Mode == ModeNone {
		return nil
	}

	if n.config_.GetVerbosity() > NO_STATUS_UPDATE {
		status.Info("Jobserver mode detected: %s", makeflags)
	}

	client, err := CreateClient(config)
	if err != nil {
		if n.config_.GetVerbosity() > QUIET {
			status.Error("Could not initialize jobserver: %v", err)
		}
		return nil
	}
	return client
}

// RunBuild 执行构建流程，返回退出状态码。
func (n *NinjaMain) RunBuild(args []string, status Status) ExitStatus {
	// 收集目标节点
	var err string
	var targets []*Node
	n.CollectTargetsFromArgs(args, &targets, &err)
	if err != "" {
		status.Error("%v", err)
		return ExitFailure
	}

	// 允许 stat 缓存（根据调试标志）
	n.disk_interface_.AllowStatCache(g_experimental_statcache)

	// 设置 jobserver 客户端（如果需要）
	jobserverClient := n.SetupJobserverClient(status)

	// 创建 Builder
	ibuilder := NewBuilder(n.state_, n.config_, n.build_log_, n.deps_log_, n.start_time_millis_, n.disk_interface_, status)
	if jobserverClient != nil {
		ibuilder.SetJobserverClient(jobserverClient)
	}

	// 添加目标到构建计划
	for _, target := range targets {
		if !ibuilder.AddTarget(target, &err) {
			if err != "" {
				status.Error("%s", err)
				return ExitFailure
			} else {
				// Added a target that is already up-to-date; not really
				// an error.
			}
		}
	}

	// 禁用 stat 缓存，避免 restat 规则看到过时的时间戳
	n.disk_interface_.AllowStatCache(false)

	// 检查是否已是最新
	if ibuilder.AlreadyUpToDate() {
		if n.config_.GetVerbosity() != NO_STATUS_UPDATE {
			status.Info("no work to do.")
		}
		return ExitSuccess
	}

	// 执行构建
	var buildErr string
	exitStatus := ibuilder.Build(&buildErr)
	if exitStatus != ExitSuccess {
		if buildErr != "" {
			status.Info("build stopped: %v.", buildErr)
			if strings.Contains(buildErr, "interrupted by user") {
				return ExitInterrupted
			}
		}
	}
	return exitStatus
}

// DeferGuessParallelism 延迟猜测并行度的辅助结构。
type DeferGuessParallelism struct {
	needGuess bool
	config    *BuildConfig
}

// NewDeferGuessParallelism 创建实例，needGuess 初始为 true。
func NewDeferGuessParallelism(config *BuildConfig) *DeferGuessParallelism {
	return &DeferGuessParallelism{
		needGuess: true,
		config:    config,
	}
}

// Refresh 如果 needGuess 为 true，则设置 config.parallelism 并标记为 false。
func (d *DeferGuessParallelism) Refresh() {
	if d.needGuess {
		d.needGuess = false
		d.config.SetParallelism(guessParallelism())
	}
}

// Close 在 defer 中调用，类似于析构函数。
func (d *DeferGuessParallelism) Close() {
	d.Refresh()
}

// readFlags 解析命令行参数，返回退出码（-1 表示继续，0 表示正常退出，1 表示错误）和剩余参数。
func readFlags(args []string, options *Options, config *BuildConfig) (int, []string) {
	deferGuess := NewDeferGuessParallelism(config)
	defer deferGuess.Close()

	// 模拟 getopt_long 的选项处理
	var remaining []string
	i := 0
	for i < len(args) && options.tool == nil {
		arg := args[i]
		if arg == "--help" || arg == "-h" {
			deferGuess.Refresh()
			usage(*config)
			return 0, nil
		} else if arg == "--version" {
			fmt.Println(kNinjaVersion)
			return 0, nil
		} else if arg == "-v" || arg == "--verbose" {
			config.SetVerbosity(VERBOSE)
		} else if arg == "--quiet" {
			config.SetVerbosity(NO_STATUS_UPDATE)
		} else if arg == "-n" {
			config.SetDryRun(true)
			config.SetDisableJobserverClient(true)
		} else if strings.HasPrefix(arg, "-d") {
			// 支持 -d MODE 或 -dMODE
			mode := strings.TrimPrefix(arg, "-d")
			if mode == "" {
				i++
				if i >= len(args) {
					fmt.Fprintln(os.Stderr, "ninja: -d requires an argument")
					return 1, nil
				}
				mode = args[i]
			}
			if !debugEnable(mode) {
				return 1, nil
			}
		} else if strings.HasPrefix(arg, "-w") {
			warn := strings.TrimPrefix(arg, "-w")
			if warn == "" {
				i++
				if i >= len(args) {
					fmt.Fprintln(os.Stderr, "ninja: -w requires an argument")
					return 1, nil
				}
				warn = args[i]
			}
			if !warningEnable(warn, options) {
				return 1, nil
			}
		} else if arg == "-C" {
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "ninja: -C requires a directory")
				return 1, nil
			}
			options.working_dir = args[i]
		} else if arg == "-f" {
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "ninja: -f requires a file")
				return 1, nil
			}
			options.input_file = args[i]
		} else if arg == "-j" {
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "ninja: -j requires a number")
				return 1, nil
			}
			value, err := strconv.ParseInt(args[i], 10, 0)
			if err != nil || value < 0 {
				fmt.Fprintln(os.Stderr, "ninja: invalid -j parameter")
				return 1, nil
			}
			if value == 0 {
				config.SetParallelism(int(^uint(0) >> 1)) // MaxInt
			} else {
				config.SetParallelism(int(value))
			}
			config.SetDisableJobserverClient(true)
			deferGuess.needGuess = false
		} else if arg == "-k" {
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "ninja: -k requires a number")
				return 1, nil
			}
			value, err := strconv.ParseInt(args[i], 10, 0)
			if err != nil {
				fmt.Fprintln(os.Stderr, "ninja: -k parameter not numeric; did you mean -k 0?")
				return 1, nil
			}
			if value == 0 {
				config.SetFailuresAllowed(int(^uint(0) >> 1))
			} else {
				config.SetFailuresAllowed(int(value))
			}
		} else if arg == "-l" {
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "ninja: -l requires a number")
				return 1, nil
			}
			value, err := strconv.ParseFloat(args[i], 64)
			if err != nil {
				fmt.Fprintln(os.Stderr, "ninja: -l parameter not numeric: did you mean -l 0.0?")
				return 1, nil
			}
			config.SetMaxLoadAverage(value)
		} else if arg == "-t" {
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "ninja: -t requires a tool name")
				return 1, nil
			}
			toolName := args[i]
			options.tool = ChooseTool(toolName)
			if options.tool == nil {
				return 0, nil
			}
			// 工具后的所有参数都保留给工具本身
			remaining = append(remaining, args[i+1:]...)
			i = len(args) // 停止解析
			break
		} else if strings.HasPrefix(arg, "-") {
			fmt.Fprintf(os.Stderr, "ninja: unknown option %s\n", arg)
			return 1, nil
		} else {
			// 非选项参数，视为目标，保留
			remaining = append(remaining, arg)
		}
		i++
	}
	// 如果还没有遇到 -t，则剩余参数就是 remaining
	if options.tool == nil {
		remaining = append(remaining, args[i:]...)
	}
	return -1, remaining
}

// realMain 是程序的主入口，负责解析参数、执行子工具或主构建流程。
// 返回退出码（0 成功，非零失败）。
func realMain() int {
	config := DefaultBuildConfig()
	options := &Options{
		input_file: "build.ninja",
	}

	// 设置 stdout 为行缓冲（Go 默认就是行缓冲，无需额外设置）
	ninjaCommand := os.Args[0]

	// 解析参数
	exitCode, remainingArgs := readFlags(os.Args[1:], options, &config)
	if exitCode >= 0 {
		return exitCode
	}

	// 创建状态输出
	status := NewStatusPrinter(config)

	// 切换工作目录
	if options.working_dir != "" {
		if options.tool == nil && config.GetVerbosity() != NO_STATUS_UPDATE {
			status.Info("Entering directory `%s'", options.working_dir)
		}
		if err := os.Chdir(options.working_dir); err != nil {
			fmt.Fprintf(os.Stderr, "ninja: chdir to '%s' - %v\n", options.working_dir, err)
			return 1
		}
	}

	// 如果工具需要在加载 manifest 前运行（RUN_AFTER_FLAGS）
	if options.tool != nil {
		if options.tool.when == RUN_AFTER_FLAGS {
			ninja := &NinjaMain{
				ninja_command_: ninjaCommand,
				config_:        &config,
				// 其他字段稍后初始化，但此类工具通常不需要完整 state
			}
			// 调用工具函数，传递剩余参数（工具本身不包含在 remainingArgs 中）
			return options.tool.f(ninja, options, remainingArgs)
		}
	}

	// 主循环：限制重建次数，防止无限循环
	const kCycleLimit = 100
	for cycle := 1; cycle <= kCycleLimit; cycle++ {
		ninja := &NinjaMain{
			ninja_command_:     ninjaCommand,
			config_:            &config,
			state_:             NewState(),
			disk_interface_:    NewRealFileSystem(),
			build_log_:         NewBuildLog(".ninja_log"),
			deps_log_:          NewDepsLog(".ninja_deps"),
			start_time_millis_: time.Now().UnixNano() / 1e6,
		}

		// 解析清单文件
		parserOpts := ManifestParserOptions{
			PhonyCycleAction: PhonyCycleActionWarn,
		}
		if options.phony_cycle_should_err {
			parserOpts.PhonyCycleAction = PhonyCycleActionError
		}
		manifestParser := NewManifestParser(ninja.state_, ninja.disk_interface_, parserOpts)
		var err string
		if !manifestParser.Load(options.input_file, &err, nil) {
			status.Error("%v", err)
			return 1
		}

		// 如果工具需要在加载后运行（RUN_AFTER_LOAD）
		if options.tool != nil {
			if options.tool != nil && options.tool.when == RUN_AFTER_LOAD {
				return options.tool.f(ninja, options, remainingArgs)
			}
		}

		// 确保构建目录存在
		if !ninja.EnsureBuildDirExists() {
			return 1
		}

		// 打开日志
		if !ninja.OpenBuildLog(false) || !ninja.OpenDepsLog(false) {
			return 1
		}

		// 如果工具需要在日志加载后运行（RUN_AFTER_LOGS）
		if options.tool != nil {
			if options.tool != nil && options.tool.when == RUN_AFTER_LOGS {
				return options.tool.f(ninja, options, remainingArgs)
			}
		}

		// 尝试重建清单文件
		rebuilt := ninja.RebuildManifest(options.input_file, &err, status)
		if err != "" {
			status.Error("rebuilding '%s': %v", options.input_file, err)
			return 1
		}
		if rebuilt {
			if config.GetDryRun() {
				return 0
			}
			// 清单已更新，重新开始循环
			continue
		}

		// 加载上次构建耗时信息
		ninja.ParsePreviousElapsedTimes()

		// 执行主构建
		exitStatus := ninja.RunBuild(remainingArgs, status)
		if g_metrics != nil {
			ninja.DumpMetrics()
		}
		return int(exitStatus)
	}

	status.Error("manifest '%s' still dirty after %d tries, perhaps system time is not set",
		options.input_file, kCycleLimit)
	return 1
}

func main() {
	// Go 没有类似 __try/__except 的异常处理，直接调用 realMain。
	// 如果需要捕获 panic，可以 defer recover，但一般不必要。
	os.Exit(realMain())
}

func usage(config BuildConfig) {
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
