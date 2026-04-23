package main

import (
	"bytes"
	"errors"
	"fmt"
	"ninja-go/ninja/util"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Subprocess 对应 C++ 中的 Subprocess 结构体
type Subprocess struct {
	cmd        *exec.Cmd
	useConsole bool          // 是否使用控制台模式（直接继承终端）
	buf        bytes.Buffer  // 合并 stdout/stderr 的输出缓冲区
	done       chan struct{} // 用于通知已完成
	mu         sync.Mutex
	exitStatus ExitStatus
	pid        int
	finished   bool
}

// SubprocessSet 对应 C++ 中的 SubprocessSet 结构体
type SubprocessSet struct {
	mu        sync.Mutex
	running   []*Subprocess    // 正在运行的子进程集合
	finishedQ []*Subprocess    // 已完成的子进程队列（FIFO）
	finishCh  chan *Subprocess // 用于接收完成通知的通道
	wg        sync.WaitGroup   // 等待所有后台 goroutine 结束
}

var (
	// 全局中断标志（原子操作）
	ginterrupted int32
	// 用于通知中断信号的通道
	sigChan = make(chan os.Signal, 1)
)

func init() {
	// 捕获常见的终止信号
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for range sigChan {
			atomic.StoreInt32(&ginterrupted, 1)
		}
	}()
}

// IsInterrupted 返回是否收到中断信号
func IsInterrupted() bool {
	return atomic.LoadInt32(&ginterrupted) != 0
}

// HandlePendingInterruption 如果中断标志被设置，则返回错误（模拟 C++ 中的类似行为）
func HandlePendingInterruption() error {
	if IsInterrupted() {
		return errors.New("interrupted by signal")
	}
	return nil
}

// NewSubprocessSet 创建并初始化 SubprocessSet
func NewSubprocessSet() *SubprocessSet {
	ps := &SubprocessSet{
		finishCh: make(chan *Subprocess, 16), // 带缓冲，避免阻塞
	}
	return ps
}

// Add 启动一个新的子进程，useConsole 为 true 时进程直接使用终端，无法捕获输出。
// 返回 *Subprocess，如果启动失败则返回 nil 并设置错误（实际使用时建议返回 error）
func (ps *SubprocessSet) Add(command string, useConsole bool) (*Subprocess, error) {
	if IsInterrupted() {
		return nil, errors.New("already interrupted")
	}

	sub := &Subprocess{
		useConsole: useConsole,
		done:       make(chan struct{}),
		// ... 其他初始化
	}

	// 设置命令（根据平台选择 shell）
	if runtime.GOOS == "windows" {
		sub.cmd = exec.Command("cmd", "/c", command)
	} else {
		sub.cmd = exec.Command("/bin/sh", "-c", command)
	}

	if useConsole {
		sub.cmd.Stdin = os.Stdin
		sub.cmd.Stdout = os.Stdout
		sub.cmd.Stderr = os.Stderr
	} else {
		sub.cmd.Stdout = &sub.buf
		sub.cmd.Stderr = &sub.buf
	}

	if err := sub.cmd.Start(); err != nil {
		return nil, err
	}
	sub.pid = sub.cmd.Process.Pid

	// 启动 goroutine 等待进程结束
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		err := sub.cmd.Wait()
		sub.mu.Lock()
		if err != nil {
			// 状态判断逻辑（同前）
			sub.exitStatus = ExitFailure
			if exiterr, ok := err.(*exec.ExitError); ok {
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					sub.exitStatus = ExitInterrupted
				}
			}
		} else {
			sub.exitStatus = ExitSuccess
		}
		sub.finished = true
		sub.mu.Unlock()
		close(sub.done)

		// 将完成的子进程发送到 finishCh
		ps.finishCh <- sub
	}()

	// 关键：判断进程是否已经快速结束
	// 使用非阻塞 select 检测 done 通道
	select {
	case <-sub.done:
		// 进程已结束，直接加入 finishedQ，不加入 running_
		ps.mu.Lock()
		ps.finishedQ = append(ps.finishedQ, sub)
		ps.mu.Unlock()
	default:
		// 进程仍在运行，加入 running_
		ps.mu.Lock()
		ps.running = append(ps.running, sub)
		ps.mu.Unlock()
	}

	return sub, nil
}

// Done 返回子进程是否已经结束（非阻塞）
func (s *Subprocess) Done() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// Finish 等待子进程结束并返回 ExitStatus
func (s *Subprocess) Finish() ExitStatus {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitStatus
}

// GetOutput 返回子进程的标准输出和标准错误合并后的内容（仅在非控制台模式下有效）
func (s *Subprocess) GetOutput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// DoWork 等待任意子进程状态变化或中断发生。
// 返回值：如果正常有进程结束返回 true；如果被中断或 context 取消返回 false。
func (ps *SubprocessSet) DoWork() bool {
	// 先检查是否有已经完成但尚未被 NextFinished 取走的进程
	// 这部分进程可能已经通过 finishCh 到达，但用户还没调用 NextFinished。
	// 我们优先处理已经排队的。
	ps.mu.Lock()
	if len(ps.finishedQ) > 0 {
		ps.mu.Unlock()
		return true
	}
	ps.mu.Unlock()

	// 如果已经中断，直接返回 false（模拟 C++ 的 interrupted）
	if IsInterrupted() {
		return false
	}

	// 等待 10ms 后再次检查中断，避免永久阻塞
	select {
	case p := <-ps.finishCh:
		ps.mu.Lock()
		ps.finishedQ = append(ps.finishedQ, p)
		for i, rp := range ps.running {
			if rp == p {
				ps.running = append(ps.running[:i], ps.running[i+1:]...)
				break
			}
		}
		ps.mu.Unlock()
		return true
	case <-time.After(10 * time.Millisecond):
		// 超时后返回 true，表示没有中断也没有新进程结束，
		// 但调用者会重新循环，从而能及时响应中断。
		return true
	}
}

// NextFinished 返回下一个已经完成的子进程，如果没有则返回 nil
func (ps *SubprocessSet) NextFinished() *Subprocess {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if len(ps.finishedQ) == 0 {
		return nil
	}
	p := ps.finishedQ[0]
	ps.finishedQ = ps.finishedQ[1:]
	return p
}

// Clear 终止所有尚未结束的子进程，并等待它们退出。
// 模拟 C++ 析构行为，释放所有资源。
func (ps *SubprocessSet) Clear() {
	// 终止所有还在运行的子进程
	ps.mu.Lock()
	runningCopy := make([]*Subprocess, len(ps.running))
	copy(runningCopy, ps.running)
	ps.mu.Unlock()

	for _, p := range runningCopy {
		if !p.Done() && p.cmd != nil && p.cmd.Process != nil {
			// Windows 直接 Kill；Unix 先 SIGTERM 再 Kill
			if runtime.GOOS == "windows" {
				p.cmd.Process.Kill()
			} else {
				p.cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-p.done:
				case <-time.After(100 * time.Millisecond):
					p.cmd.Process.Kill()
				}
			}
		}
	}
	ps.wg.Wait()

	ps.mu.Lock()
	ps.running = nil
	ps.finishedQ = nil
	ps.mu.Unlock()
}

// CheckConsoleProcessTerminated 在 C++ 版本中用于轮询控制台子进程。
// 在 Go 版本中，由于我们统一使用 goroutine + Wait，不再需要单独轮询。
// 保留此方法仅为满足接口兼容，可直接返回。
func (ps *SubprocessSet) CheckConsoleProcessTerminated() {
	// no-op in Go implementation
}

// RealCommandRunner 真实命令执行器
type RealCommandRunner struct {
	config        *BuildConfig
	jobserver     JobserverClient
	subprocs      *SubprocessSet
	subprocToEdge map[*Subprocess]*Edge
	mu            sync.Mutex
}

// Verify that *UserCacher implements Cacher
var _ CommandRunner = (*RealCommandRunner)(nil)

func NewRealCommandRunner(config *BuildConfig, jobserver JobserverClient) *RealCommandRunner {
	subprocs := NewSubprocessSet()
	return &RealCommandRunner{
		config:        config,
		jobserver:     jobserver,
		subprocs:      subprocs,
		subprocToEdge: make(map[*Subprocess]*Edge),
	}
}

func (r *RealCommandRunner) CanRunMore() int {
	subprocNumber := len(r.subprocs.running) + len(r.subprocs.finishedQ)
	capacity := r.config.parallelism - subprocNumber
	if r.jobserver != nil {
		capacity = int(^uint(0) >> 1) // 相当于 INT_MAX
	}
	if r.config.max_load_average > 0 {
		load := util.GetLoadAverage()
		loadCapacity := int(r.config.max_load_average - load)
		if loadCapacity < capacity {
			capacity = loadCapacity
		}
	}
	if capacity < 0 {
		capacity = 0
	}
	if capacity == 0 && len(r.subprocs.running) == 0 {
		capacity = 1
	}
	return capacity
}

func (r *RealCommandRunner) StartCommand(edge *Edge) bool {
	command := edge.EvaluateCommand(false)
	subproc, err := r.subprocs.Add(command, edge.use_console())
	if err != nil {
		fmt.Println(err)
		return false
	}
	r.mu.Lock()
	r.subprocToEdge[subproc] = edge
	r.mu.Unlock()
	return true
}

func (r *RealCommandRunner) WaitForCommand(result *CommandResult) bool {
	var subproc *Subprocess
	for {
		subproc = r.subprocs.NextFinished()
		if subproc != nil {
			break
		}
		interrupted := r.subprocs.DoWork()
		if interrupted {
			result.Status = ExitInterrupted
			return false
		}
	}

	result.Status = subproc.Finish()
	result.Output = subproc.GetOutput()

	e := r.subprocToEdge[subproc]
	result.Edge = e
	delete(r.subprocToEdge, subproc)

	// Subprocess cleanup: let GC handle it, or call a Close method if needed.
	// In Go, we typically don't explicitly delete objects.
	return true
}

func (r *RealCommandRunner) GetActiveEdges() []*Edge {
	r.mu.Lock()
	defer r.mu.Unlock()
	edges := make([]*Edge, 0, len(r.subprocToEdge))
	for _, e := range r.subprocToEdge {
		edges = append(edges, e)
	}
	return edges
}

func (r *RealCommandRunner) Abort() {
	r.clearJobTokens()
	r.subprocs.Clear()
}

func (r *RealCommandRunner) clearJobTokens() {
	if r.jobserver != nil {
		for _, edge := range r.GetActiveEdges() {
			r.jobserver.Release(edge.job_slot_)
		}
	}
}
