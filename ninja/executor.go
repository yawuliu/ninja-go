package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

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
func (e *Executor) Run(edges []*Edge, buildEdge func(e *Edge) error) error {
	if len(edges) == 0 {
		return nil
	}

	// 构建依赖关系图
	dependedBy := make(map[*Edge][]*Edge)
	inDegree := make(map[*Edge]int)
	for _, e1 := range edges {
		for _, out := range e1.outputs_ {
			for _, e2 := range edges {
				for _, in := range e2.inputs_ {
					if in == out {
						dependedBy[e1] = append(dependedBy[e1], e2)
						inDegree[e2]++
					}
				}
			}
		}
	}

	// 任务队列
	taskQueue := make(chan *Edge, len(edges))
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
				if edge.pool_ != nil {
					if err := e.acquirePool(edge.pool_.Name); err != nil {
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
				if edge.pool_ != nil {
					e.releasePool(edge.pool_.Name)
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
