package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Subprocess 对应 C++ 的 Subprocess
type Subprocess struct {
	cmd        *exec.Cmd
	useConsole bool
	buf        bytes.Buffer // 合并 stdout/stderr
	done       chan struct{}
	mu         sync.Mutex
	exitStatus ExitStatus
	pid        int
	finished   bool
}

// SubprocessSet 对应 C++ 的 SubprocessSet
type SubprocessSet struct {
	mu        sync.Mutex
	running   []*Subprocess
	finishedQ []*Subprocess
	finishCh  chan *Subprocess
	wg        sync.WaitGroup
}

// 全局中断标志 (原子操作)
var interrupted int32

// 全局信号通道 (仅用于触发标志设置)
var sigChan chan os.Signal

func init() {
	sigChan = make(chan os.Signal, 1)
	// 跨平台信号注册
	if runtime.GOOS == "windows" {
		// Windows 仅处理 Ctrl+C (SIGINT)
		signal.Notify(sigChan, os.Interrupt)
	} else {
		// Unix 处理 SIGINT, SIGTERM, SIGHUP
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	}
	go func() {
		for range sigChan {
			atomic.StoreInt32(&interrupted, 1)
		}
	}()
}

// IsInterrupted 返回是否收到中断信号
func IsInterrupted() bool {
	return atomic.LoadInt32(&interrupted) != 0
}

// NewSubprocessSet 创建 SubprocessSet
func NewSubprocessSet() *SubprocessSet {
	return &SubprocessSet{
		finishCh: make(chan *Subprocess, 64), // 足够缓冲
	}
}

// Add 启动一个子进程，行为与 C++ 版本一致
func (ps *SubprocessSet) Add(command string, useConsole bool) (*Subprocess, error) {
	if IsInterrupted() {
		return nil, errors.New("interrupted, cannot start new process")
	}

	sub := &Subprocess{
		useConsole: useConsole,
		done:       make(chan struct{}),
	}

	// 构建命令
	sub.cmd = makeCmd(command)

	// 设置标准输入输出
	if useConsole {
		// 控制台模式：直接继承终端
		sub.cmd.Stdin = os.Stdin
		sub.cmd.Stdout = os.Stdout
		sub.cmd.Stderr = os.Stderr
	} else {
		// 非控制台模式：捕获输出
		sub.cmd.Stdout = &sub.buf
		sub.cmd.Stderr = &sub.buf
	}

	// 启动进程
	if err := sub.cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start: %w", err)
	}
	sub.pid = sub.cmd.Process.Pid

	// 等待子进程结束的 goroutine
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		err := sub.cmd.Wait()
		sub.mu.Lock()
		defer sub.mu.Unlock()
		if err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					sub.exitStatus = ExitInterrupted
				} else {
					sub.exitStatus = ExitFailure
				}
			} else {
				sub.exitStatus = ExitFailure
			}
		} else {
			sub.exitStatus = ExitSuccess
		}
		sub.finished = true
		close(sub.done)
		ps.finishCh <- sub
	}()

	// 关键：检测进程是否已经快速结束（模拟 C++ 的 child_ 判断）
	select {
	case <-sub.done:
		// 进程已结束，直接放入 finishedQ
		ps.mu.Lock()
		ps.finishedQ = append(ps.finishedQ, sub)
		ps.mu.Unlock()
	default:
		// 进程还在运行，加入 running
		ps.mu.Lock()
		ps.running = append(ps.running, sub)
		ps.mu.Unlock()
	}
	return sub, nil
}

// Done 返回子进程是否已完成
func (s *Subprocess) Done() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// Finish 等待子进程结束并返回状态
func (s *Subprocess) Finish() ExitStatus {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitStatus
}

// GetOutput 返回捕获的输出（仅非控制台模式有效）
func (s *Subprocess) GetOutput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// DoWork 等待任意子进程状态变化，或检测中断。
// 返回值：true 表示有进程结束或中断尚未发生（调用者应继续循环），
//
//	false 表示出发了中断（调用者应退出）。
func (ps *SubprocessSet) DoWork() bool {
	// 如果全局中断标志已设置，则不再等待，直接返回 false 指示退出
	if IsInterrupted() {
		return false
	}

	// 检查 finishedQ 是否已有进程（避免在无运行时阻塞）
	ps.mu.Lock()
	if len(ps.finishedQ) > 0 {
		ps.mu.Unlock()
		return true
	}
	ps.mu.Unlock()

	// 等待子进程结束，或定期醒来检查中断
	select {
	case p := <-ps.finishCh:
		// 收到完成的子进程，将其从 running 移到 finishedQ
		ps.mu.Lock()
		// 从 running 中移除
		for i, rp := range ps.running {
			if rp == p {
				ps.running = append(ps.running[:i], ps.running[i+1:]...)
				break
			}
		}
		ps.finishedQ = append(ps.finishedQ, p)
		ps.mu.Unlock()
		return true

	case <-time.After(10 * time.Millisecond):
		// 定期醒来，让调用者有机会再次检查中断标志
		// 若在此期间中断标志被设置，下次 DoWork 会返回 false
		return true
	}
}

// NextFinished 返回下一个已完成的子进程，若无则返回 nil
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

// Clear 终止所有未完成的子进程并清理资源
func (ps *SubprocessSet) Clear() {
	// 先复制 running 列表，避免锁内长时间操作
	ps.mu.Lock()
	runningCopy := make([]*Subprocess, len(ps.running))
	copy(runningCopy, ps.running)
	ps.mu.Unlock()

	for _, p := range runningCopy {
		if !p.Done() && p.cmd != nil && p.cmd.Process != nil {
			if runtime.GOOS == "windows" {
				// Windows 直接 Kill
				p.cmd.Process.Kill()
			} else {
				// Unix 先尝试 SIGTERM，再 SIGKILL
				p.cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-p.done:
				case <-time.After(100 * time.Millisecond):
					p.cmd.Process.Kill()
				}
			}
		}
	}
	// 等待所有 goroutine 退出
	ps.wg.Wait()

	// 清空内部状态
	ps.mu.Lock()
	ps.running = nil
	ps.finishedQ = nil
	ps.mu.Unlock()
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
		load := GetLoadAverage()
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
		ninterrupted := r.subprocs.DoWork()
		if !ninterrupted {
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
