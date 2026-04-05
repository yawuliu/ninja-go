package executor

import (
	"fmt"
	"ninja-go/pkg/graph"
	"sync"
	"sync/atomic"
)

// CommandRunner 定义命令执行接口
type CommandRunner interface {
	Run(edge *graph.Edge, expandFunc func(*graph.Edge) (string, error)) error
}

// Executor 管理构建任务的并行执行和资源池
type Executor struct {
	parallel int
	pools    map[string]*PoolState // pool name -> current running count
	mu       sync.Mutex
}

type PoolState struct {
	Depth int
	Curr  int
	Cond  *sync.Cond
}

func NewExecutor(parallel int) *Executor {
	return &Executor{
		parallel: parallel,
		pools:    make(map[string]*PoolState),
	}
}

// RegisterPool 注册一个池（深度限制）
func (e *Executor) RegisterPool(name string, depth int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pools[name] = &PoolState{
		Depth: depth,
		Curr:  0,
		Cond:  sync.NewCond(&e.mu),
	}
}

// Run 并行执行一组边，按依赖顺序自动调度
func (e *Executor) Run(edges []*graph.Edge, buildEdge func(e *graph.Edge) error) error {
	if len(edges) == 0 {
		return nil
	}

	// 构建依赖关系图
	dependedBy := make(map[*graph.Edge][]*graph.Edge)
	inDegree := make(map[*graph.Edge]int)
	for _, e1 := range edges {
		for _, out := range e1.Outputs {
			for _, e2 := range edges {
				for _, in := range e2.Inputs {
					if in == out {
						dependedBy[e1] = append(dependedBy[e1], e2)
						inDegree[e2]++
					}
				}
			}
		}
	}

	// 任务队列
	taskQueue := make(chan *graph.Edge, len(edges))
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	// 原子计数器：已完成的边数
	var completed int32
	total := int32(len(edges))

	// 启动 workers
	for i := 0; i < e.parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for edge := range taskQueue {
				// 等待池许可
				if edge.Pool != nil {
					if err := e.acquirePool(edge.Pool.Name); err != nil {
						select {
						case errCh <- err:
						default:
						}
						return
					}
				}
				// 执行命令
				if err := buildEdge(edge); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				// 释放池
				if edge.Pool != nil {
					e.releasePool(edge.Pool.Name)
				}
				// 唤醒依赖它的边
				e.mu.Lock()
				for _, depEdge := range dependedBy[edge] {
					inDegree[depEdge]--
					if inDegree[depEdge] == 0 {
						taskQueue <- depEdge
					}
				}
				e.mu.Unlock()
				if atomic.AddInt32(&completed, 1) == total {
					// 所有边完成，关闭队列
					close(taskQueue)
				}
				// 注意：这里 close 可能会导致其他 worker 在 range 上退出，但我们需要确保 close 只执行一次。
				// 更好的做法是在外部 goroutine 中监控 completed，而不是在 worker 内关闭。
			}
		}()
	}

	// 将入度为0的边放入队列
	for _, edge := range edges {
		if inDegree[edge] == 0 {
			taskQueue <- edge
		}
	}

	// 监控 goroutine：等待所有 worker 完成，然后关闭队列和 done channel
	done := make(chan struct{})
	go func() {
		wg.Wait()
		// close(taskQueue)
		close(done)
	}()

	// 等待错误或完成
	select {
	case err := <-errCh:
		return err
	case <-done:
		return nil
	}
}

func (e *Executor) acquirePool(poolName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	ps, ok := e.pools[poolName]
	if !ok {
		return fmt.Errorf("unknown pool: %s", poolName)
	}
	for ps.Curr >= ps.Depth {
		ps.Cond.Wait()
	}
	ps.Curr++
	return nil
}

func (e *Executor) releasePool(poolName string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ps := e.pools[poolName]
	ps.Curr--
	ps.Cond.Signal()
}

/*
func (e *Executor) buildEdge(fs util.FileSystem, cmdRunner util.CommandRunner, edge *graph.Edge, expandFunc func(*graph.Edge) (string, error)) error {
	// 检查所有输入是否存在：如果缺失且没有生成它的边，则报错
	for _, in := range edge.Inputs {
		if err := in.LoadMtime(fs); err != nil {
			return err
		}
		if in.Mtime == -1 && in.Edge == nil {
			// return fmt.Errorf("missing input file: %s", in.Path)
			return fmt.Errorf("missing input file and no rule to generate it: %s", in.Path)
		}
	}
	// 同样检查隐式依赖
	for _, imp := range edge.ImplicitDeps {
		if err := imp.LoadMtime(fs); err != nil {
			return err
		}
		if imp.Mtime == -1 && imp.Edge == nil {
			return fmt.Errorf("missing implicit dependency and no rule: %s", imp.Path)
		}
	}

	start := time.Now().UnixNano()
	cmdLine, err := expandFunc(edge)
	if err != nil {
		return err
	}
	fmt.Printf("[build] %s\n", cmdLine)

	var stdoutBuf, stderrBuf bytes.Buffer
	if runtime.GOOS == "windows" {
		//cmd.Stdout = &stdoutBuf
		//cmd.Stderr = &stderrBuf
		err = cmdRunner.Run(cmdLine, &stdoutBuf, &stderrBuf)
	} else {
		//cmd.Stdout = os.Stdout
		//cmd.Stderr = os.Stderr
		err = cmdRunner.Run(cmdLine, os.Stdout, os.Stderr)
	}
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
	//cmd := util.CommandForShell(cmdLine)
	//cmd.Stdout = os.Stdout
	//cmd.Stderr = os.Stderr
	//if err := cmd.Run(); err != nil {
	//	return fmt.Errorf("command failed: %v", err)
	//}
	end := time.Now().UnixNano()
	// 更新输出文件的时间戳
	// 记录到 buildlog
	for _, out := range edge.Outputs {
		_ = out.LoadMtime(fs)
		rec := &buildlog.Record{
			Output:      out.Path,
			CommandHash: graph.ComputeCommandHash(edge),
			StartTime:   start,
			EndTime:     end,
		}
		b.buildLog.UpdateRecord(rec)
	}
	// 如果有 depfile，解析并更新隐式依赖
	if edge.Rule.Depfile != "" {
		if err := b.parseDepfile(edge); err != nil {
			return fmt.Errorf("depfile error: %v", err)
		}
	}
	if edge.Rule.Dyndep != "" {
		// 展开 dyndep 路径中的变量（例如 $out）
		dyndepPath := strings.ReplaceAll(edge.Rule.Dyndep, "$out", edge.Outputs[0].Path)
		if err := b.processDyndep(dyndepPath); err != nil {
			return fmt.Errorf("dyndep error: %v", err)
		}
	}
	return nil
}
*/
