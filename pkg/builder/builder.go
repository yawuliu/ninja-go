package builder

import (
	"bytes"
	"errors"
	"fmt"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"io"
	"ninja-go/pkg/buildlog"
	"ninja-go/pkg/depslog"
	"ninja-go/pkg/dyndep"
	"ninja-go/pkg/executor"
	"ninja-go/pkg/plan"
	"ninja-go/pkg/util"
	"os"
	"runtime"
	"strings"
	"time"

	"ninja-go/pkg/graph"
)

// FileSystem 文件系统接口

// RealCommandRunner 真实命令执行器
type RealCommandRunner struct{}

func (r *RealCommandRunner) Run(cmdLine string, stdout, stderr io.Writer) error {
	cmd := util.CommandForShell(cmdLine)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// RealFileSystem 真实文件系统
type RealFileSystem struct{}

func (fs *RealFileSystem) Open(path string) (util.File, error) {
	return os.Open(path)
}
func (fs *RealFileSystem) Create(path string) (util.File, error) {
	return os.Create(path)
}
func (fs *RealFileSystem) Truncate(name string, size int64) error {
	return os.Truncate(name, size)
}

func (fs *RealFileSystem) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (fs *RealFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (fs *RealFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (fs *RealFileSystem) Remove(path string) error {
	return os.Remove(path)
}

func (fs *RealFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

type Builder struct {
	state     *graph.State
	parallel  int
	depsLog   *depslog.DepsLog // 新增
	buildLog  *buildlog.BuildLog
	executor  *executor.Executor
	newEdges  []*graph.Edge
	cmdRunner util.CommandRunner
	fs        util.FileSystem
}

func NewBuilder(state *graph.State, parallel int, cmdRunner util.CommandRunner, fs util.FileSystem) *Builder {
	if cmdRunner == nil {
		cmdRunner = &RealCommandRunner{}
	}
	if fs == nil {
		fs = &RealFileSystem{}
	}
	return &Builder{
		state:     state,
		parallel:  parallel,
		depsLog:   depslog.NewDepsLog(".ninja_deps"),
		buildLog:  buildlog.NewBuildLog(".ninja_log"),
		executor:  executor.NewExecutor(parallel),
		cmdRunner: cmdRunner,
		fs:        fs,
	}
}

func (b *Builder) Build(targets []string) error {
	// 加载已有的依赖缓存
	if err := b.depsLog.Load(b.state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: load .ninja_deps: %v\n", err)
	}
	if err := b.buildLog.Load(b.fs); err != nil {
		fmt.Fprintf(os.Stderr, "warning: load .ninja_log: %v\n", err)
	}
	defer func() { // 构建结束后保存
		b.depsLog.Close()
		b.buildLog.Save()
	}()

	// 转换目标为节点
	//var nodes []*graph.Node
	//if len(targets) == 1 && targets[0] == "default" {
	//	if len(b.state.Defaults) == 0 {
	//		// 没有 default 语句，则构建所有边的输出
	//		var allTargets []*graph.Node
	//		for _, edge := range b.state.Edges {
	//			for _, out := range edge.Outputs {
	//				allTargets = append(allTargets, out)
	//			}
	//		}
	//		nodes = allTargets
	//	} else {
	//		nodes = b.state.Defaults
	//	}
	//} else {
	//	// 转换指定的目标为节点
	//	for _, t := range targets {
	//		n := b.state.AddNode(t)
	//		nodes = append(nodes, n)
	//	}
	//}
	//for _, t := range targets {
	//	if t == "default" {
	//		if len(b.state.Defaults) == 0 {
	//			return errors.New("no default targets")
	//		}
	//		nodes = append(nodes, b.state.Defaults...)
	//	} else {
	//		n := b.state.AddNode(util.NormalizePath(t))
	//		nodes = append(nodes, n)
	//	}
	//}
	// 获取初始构建计划
	nodes, err := b.resolveTargets(targets)
	if err != nil {
		return err
	}
	// 生成计划
	p := plan.NewPlan(b.state, b.buildLog)
	edges, err := p.Compute(b.fs, nodes)
	if err != nil {
		return err
	}
	// 第一阶段：处理所有需要生成 dyndep 文件但文件不存在的边
	dyndepEdges, err := b.prepareDyndeps(edges)
	if err != nil {
		return err
	}
	if len(dyndepEdges) > 0 {
		fmt.Printf("ninja: generating dyndep files...\n")
		// 顺序执行这些 dyndep 边（不能用并行，因为可能相互依赖）
		for _, e := range dyndepEdges {
			if err := b.buildEdge(e); err != nil {
				return fmt.Errorf("building dyndep edge %v: %v", e.Outputs, err)
			}
		}
		// 将已构建的dyndep边标记为clean
		//for _, e := range dyndepEdges {
		//	for _, out := range e.Outputs {
		//		if err := out.LoadMtime(b.fs); err != nil {
		//			return err
		//		}
		//		out.Dirty = false
		//	}
		//}
		// 重新加载所有 dyndep 文件，将新边加入 state
		if err := b.loadAllDyndeps(edges); err != nil {
			return err
		}
		// 重新计划（包括新边）
		// 重新计划
		// p := plan.NewPlan(b.state, b.buildLog)
		edges, err = p.Compute(b.fs, nodes)
		if err != nil {
			return err
		}
	}

	if len(edges) == 0 {
		fmt.Println("ninja: no work to do.")
		return nil
	}
	// 并行执行
	return b.runEdges(edges)
}

// resolveTargets 将目标字符串转换为节点列表
func (b *Builder) resolveTargets(targets []string) ([]*graph.Node, error) {
	var nodes []*graph.Node
	for _, t := range targets {
		if t == "default" {
			if len(b.state.Defaults) == 0 {
				return nil, errors.New("no default targets")
			}
			nodes = append(nodes, b.state.Defaults...)
		} else {
			n := b.state.AddNode(t)
			nodes = append(nodes, n)
		}
	}
	return nodes, nil
}

// prepareDyndeps 找出所有需要生成 dyndep 文件但文件不存在的边
func (b *Builder) prepareDyndeps(edges []*graph.Edge) ([]*graph.Edge, error) {
	// 使用 map 去重
	needed := make(map[*graph.Edge]bool)
	for _, e := range edges {
		if e.DyndepFile != nil {
			// 检查 dyndep 文件是否存在
			if _, err := b.fs.Stat(e.DyndepFile.Path); os.IsNotExist(err) {
				// 找出生成该 dyndep 文件的边
				if e.DyndepFile.Edge != nil {
					needed[e.DyndepFile.Edge] = true
				} else {
					return nil, fmt.Errorf("dyndep file %s missing and no rule to build it", e.DyndepFile.Path)
				}
			}
		}
	}
	var result []*graph.Edge
	for e := range needed {
		result = append(result, e)
	}
	return result, nil
	//var result []*graph.Edge
	//for _, e := range edges {
	//	if e.DyndepFile != nil {
	//		// 检查 dyndep 文件是否存在
	//		if _, err := b.fs.Stat(e.DyndepFile.Path); os.IsNotExist(err) {
	//			result = append(result, e)
	//		}
	//	}
	//	//if e.Rule.Dyndep != "" {
	//	//	// 展开 dyndep 路径中的变量
	//	//	dyndepPath := strings.ReplaceAll(e.Rule.Dyndep, "$out", e.Outputs[0].Path)
	//	//	if _, err := b.fs.Stat(dyndepPath); os.IsNotExist(err) {
	//	//		result = append(result, e)
	//	//	}
	//	//}
	//}
	//return result, nil
}

// loadAllDyndeps 加载所有边中引用的 dyndep 文件（假设文件已存在）
func (b *Builder) loadAllDyndeps(edges []*graph.Edge) error {
	for _, e := range edges {
		if e.DyndepFile != nil {
			if _, err := b.fs.Stat(e.DyndepFile.Path); err == nil {
				loader := dyndep.NewDyndepLoader(b.state)
				if err := loader.LoadFromFile(b.fs, e.DyndepFile.Path); err != nil {
					return fmt.Errorf("load dyndep %s: %v", e.DyndepFile.Path, err)
				}
				if err := loader.Apply(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (b *Builder) runEdges(edges []*graph.Edge) error {
	// 注册所有使用的池
	fmt.Printf("runEdges: %d edges\n", len(edges))
	for i, e := range edges {
		fmt.Printf("  edge %d: outputs=%v\n", i, e.Outputs)
		if e.Pool != nil {
			b.executor.RegisterPool(e.Pool.Name, e.Pool.Depth)
		}
	}
	//expandFunc := func(edge *graph.Edge) (string, error) {
	//	cmd, _, err := b.expandCommand(edge) // 复用现有展开逻辑
	//	return cmd, err
	//}
	return b.executor.Run(edges, b.buildEdge)
	/*// 拓扑排序已保证顺序，直接串行执行
	for i, e := range edges {
		fmt.Printf("[%d/%d] building %v\n", i+1, len(edges), e.Outputs[0].Path)
		if err := b.buildEdge(e); err != nil {
			return err
		}
	}
	return nil
	// 构建依赖映射：边 -> 依赖它的边列表（用于唤醒）
	dependedBy := make(map[*graph.Edge][]*graph.Edge)
	inDegree := make(map[*graph.Edge]int)
	for _, e := range edges {
		for _, out := range e.Outputs {
			for _, other := range edges {
				for _, in := range other.Inputs {
					if in == out {
						dependedBy[e] = append(dependedBy[e], other)
						inDegree[other]++
					}
				}
			}
		}
	}
	// 初始化就绪队列
	ready := make(chan *graph.Edge, len(edges))
	var mu sync.Mutex // 保护 inDegree 和 ready 的并发修改
	for _, e := range edges {
		if inDegree[e] == 0 {
			ready <- e
		}
	}
	// 工作池
	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	done := make(chan struct{})

	// 启动 workers
	for i := 0; i < b.parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range ready {
				// 构建当前边
				if err := b.buildEdge(e); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				// 唤醒依赖它的边
				mu.Lock()
				for _, depEdge := range dependedBy[e] {
					inDegree[depEdge]--
					if inDegree[depEdge] == 0 {
						ready <- depEdge
					}
				}
				mu.Unlock()
			}
		}()
	}
	// 等待完成或错误
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case err := <-errCh:
		return err
	case <-done:
		return nil
	}*/
}

func (b *Builder) buildEdge(e *graph.Edge) error {
	// 检查所有输入是否存在：如果缺失且没有生成它的边，则报错
	for _, in := range e.Inputs {
		if err := in.LoadMtime(b.fs); err != nil {
			return err
		}
		if in.Mtime == -1 && in.Edge == nil {
			// return fmt.Errorf("missing input file: %s", in.Path)
			return fmt.Errorf("missing input file and no rule to generate it: %s", in.Path)
		}
	}
	// 同样检查隐式依赖
	for _, imp := range e.ImplicitDeps {
		if err := imp.LoadMtime(b.fs); err != nil {
			return err
		}
		if imp.Mtime == -1 && imp.Edge == nil {
			return fmt.Errorf("missing implicit dependency and no rule: %s", imp.Path)
		}
	}
	start := time.Now().UnixNano()
	cmdLine, responseFiles, err := b.expandCommand(e)
	if err != nil {
		return err
	}
	if cmdLine != "" {
		// 生成响应文件
		for rspPath, content := range responseFiles {
			if err := b.fs.WriteFile(rspPath, []byte(content), 0644); err != nil {
				return fmt.Errorf("write response file %s: %v", rspPath, err)
			}
			defer b.fs.Remove(rspPath)
		}
		// 简化进度
		fmt.Printf("[build] %s\n", cmdLine)

		// 其他内建变量如 $depfile 等类似
		// cmd := util.CommandForShell(cmdLine)
		var stdoutBuf, stderrBuf bytes.Buffer
		if runtime.GOOS == "windows" {
			//cmd.Stdout = &stdoutBuf
			//cmd.Stderr = &stderrBuf
			err = b.cmdRunner.Run(cmdLine, &stdoutBuf, &stderrBuf)
		} else {
			//cmd.Stdout = os.Stdout
			//cmd.Stderr = os.Stderr
			err = b.cmdRunner.Run(cmdLine, os.Stdout, os.Stderr)
		}
		// 强制命令使用英文输出（避免编码问题）
		// cmd.Env = append(os.Environ(), "LC_ALL=C")
		//if runtime.GOOS == "windows" {
		// 设置环境变量让命令输出UTF-8
		// cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "GIT_PAGER=")
		// 或者直接使用系统编码转换，但更简单是让控制台支持UTF-8
		// 可以在命令前加 chcp 65001 > nul  &&
		// 但更简单：修改util.CommandForShell，在cmdline前加 chcp 65001 > nul &&
		//}

		// err = cmd.Run()
		if runtime.GOOS == "windows" {
			// 转换 GBK 到 UTF-8 后输出
			decoder := simplifiedchinese.GBK.NewDecoder()
			if stdoutBuf.Len() > 0 {
				utf8Out, _, _ := transform.Bytes(decoder, stdoutBuf.Bytes())
				os.Stdout.Write(utf8Out)
			}
			if stderrBuf.Len() > 0 {
				utf8Err, _, _ := transform.Bytes(decoder, stderrBuf.Bytes())
				os.Stderr.Write(utf8Err)
			}
		}
		if err != nil {
			return fmt.Errorf("command failed: %v", err)
		}
	}
	end := time.Now().UnixNano()
	// 更新输出文件的时间戳
	// 记录到 buildlog
	for _, out := range e.Outputs {
		_ = out.LoadMtime(b.fs)
		out.Dirty = false
		rec := &buildlog.Record{
			Output:      out.Path,
			CommandHash: graph.ComputeCommandHash(e),
			StartTime:   start,
			EndTime:     end,
		}
		b.buildLog.UpdateRecord(rec)
	}

	// 如果有 depfile，解析并更新隐式依赖
	if e.Rule.Depfile != "" {
		if err := b.parseDepfile(e); err != nil {
			return fmt.Errorf("depfile error: %v", err)
		}
	}
	if e.Rule.Dyndep != "" {
		// 展开 dyndep 路径中的变量（例如 $out）
		dyndepPath := strings.ReplaceAll(e.Rule.Dyndep, "$out", e.Outputs[0].Path)
		if err := b.processDyndep(dyndepPath); err != nil {
			return fmt.Errorf("dyndep error: %v", err)
		}
	}

	return nil
}

// 新增方法 processDyndep
func (b *Builder) processDyndep(dyndepPath string) error {
	// 读取 dyndep 文件内容
	data, err := b.fs.ReadFile(dyndepPath)
	if err != nil {
		return fmt.Errorf("read dyndep file: %v", err)
	}
	loader := dyndep.NewDyndepLoader(b.state)
	if err := loader.LoadFromContent(dyndepPath, string(data)); err != nil {
		return err
	}
	if err := loader.Apply(); err != nil {
		return err
	}
	// 重新标记受影响的节点为脏（因为新增了隐式输入/输出）
	// 这里需要重新运行脏标记逻辑，简单做法是重新计划整个图
	// 由于我们在两阶段中会重新调用 plan.Compute，因此无需额外处理
	return nil
	//newEdges, err := dyndep.LoadAndAddEdges(b.state, dyndepPath)
	//if err != nil {
	//	return err
	//}
	//if len(newEdges) == 0 {
	//	return nil
	//}
	// 对于新添加的边，需要重新标记脏状态并可能继续构建
	// 这里简单地将新边加入当前构建队列（假设当前是顺序执行）
	// 如果是并行执行，需要更复杂的动态调度。
	// 由于我们当前使用顺序执行，可以直接在后续继续处理这些新边。
	// 但更好的方式是重新生成计划并递归调用。
	// 为简化，我们将新边追加到当前构建列表（需要外部支持）。
	// 我们通过返回新边，由调用者（runEdges）决定如何处理。
}

// expandCommand 替换 $in, $out 等变量，并处理响应文件（@rsp）
// 返回最终的命行字符串和需要写入的响应文件映射
func (b *Builder) expandCommand(e *graph.Edge) (string, map[string]string, error) {
	fmt.Printf("DEBUG: Rule %s, Command: %q\n", e.Rule.Name, e.Rule.Command) // 添加此行
	rule := e.Rule
	cmd := rule.Command
	// 替换 $in, $out
	inList := paths(e.Inputs)
	outList := paths(e.Outputs)
	cmd = strings.ReplaceAll(cmd, "$in", strings.Join(inList, " "))
	cmd = strings.ReplaceAll(cmd, "$out", strings.Join(outList, " "))
	// 处理其他内建变量如 $depfile, $dyndep 等（简单替换）
	if rule.Depfile != "" {
		depfile := strings.ReplaceAll(rule.Depfile, "$out", e.Outputs[0].Path) // outList[0]
		cmd = strings.ReplaceAll(cmd, "$depfile", depfile)
	}
	// 处理 $$ 转义为 $
	cmd = strings.ReplaceAll(cmd, "$$", "$")
	// 响应文件支持：查找 @rsp 或 @文件名
	responseFiles := make(map[string]string)
	// 简单检测：如果命令包含 @ 且后面跟着一个文件（无空格），则假定需要生成响应文件
	// 实际更严谨的做法是解析命令行，但这里简化
	// 用户需要自己在 rule 中写 @out.rsp 或类似
	// 我们实现一个通用替换：将 @$out.rsp 替换为临时文件路径
	if strings.Contains(cmd, "@$out.rsp") {
		rspPath := outList[0] + ".rsp"
		// 将输入文件列表写入响应文件（每个参数一行）
		content := strings.Join(inList, "\n")
		responseFiles[rspPath] = content
		cmd = strings.ReplaceAll(cmd, "@$out.rsp", "@"+rspPath)
	}
	// Windows 下将路径中的正斜杠转换为反斜杠（为了兼容 cmd）
	if runtime.GOOS == "windows" {
		cmd = strings.ReplaceAll(cmd, "/", "\\")
	}
	return cmd, responseFiles, nil
}

func (b *Builder) parseDepfile(e *graph.Edge) error {
	depfilePath := strings.ReplaceAll(e.Rule.Depfile, "$out", e.Outputs[0].Path)
	data, err := b.fs.ReadFile(depfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	parser := &DepfileParser{}
	if err := parser.Parse(string(data)); err != nil {
		return err
	}
	// 处理 parser.Outs 和 parser.Ins
	// 注意：Outs 可能包含多个输出，但我们只关心当前边的输出
	// 需要匹配当前边的输出，然后添加 Ins 到隐式依赖
	targetOutput := e.Outputs[0].Path
	// 检查 targetOutput 是否在 parser.Outs 中，如果在，则添加所有 Ins
	found := false
	for _, out := range parser.Outs {
		if out == targetOutput {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	for _, dep := range parser.Ins {
		depNode := b.state.AddNode(dep)
		// 去重
		found := false
		for _, existing := range e.ImplicitDeps {
			if existing == depNode {
				found = true
				break
			}
		}
		if !found {
			e.ImplicitDeps = append(e.ImplicitDeps, depNode)
		}
	}
	return nil
}

func paths(nodes []*graph.Node) []string {
	res := make([]string, len(nodes))
	for i, n := range nodes {
		res[i] = n.Path
	}
	return res
}
