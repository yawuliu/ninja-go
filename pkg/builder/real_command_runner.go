package builder

import (
	"ninja-go/pkg/util"
	"os/exec"
	"sync"
)

// Subprocess 表示一个正在运行的子进程。
type Subprocess struct {
	cmd    *exec.Cmd
	edge   *Edge
	output string
	done   chan error
}

// SubprocessSet 管理一组子进程。
type SubprocessSet struct {
	mu       sync.Mutex
	running  map[*Subprocess]bool
	finished []*Subprocess
}

func NewSubprocessSet() *SubprocessSet {
	return &SubprocessSet{
		running:  make(map[*Subprocess]bool),
		finished: []*Subprocess{},
	}
}

func (s *SubprocessSet) Add(command string, useConsole bool) *Subprocess {
	cmd := util.CommandForShell(command)
	if useConsole {
		// 设置让子进程继承控制台（在 Windows 上需要特殊处理，这里简化）
	}
	sub := &Subprocess{
		cmd:  cmd,
		done: make(chan error),
	}
	if err := cmd.Start(); err != nil {
		return nil
	}
	s.mu.Lock()
	s.running[sub] = true
	s.mu.Unlock()
	go func() {
		err := cmd.Wait()
		output, _ := cmd.CombinedOutput()
		sub.output = string(output)
		sub.done <- err
		close(sub.done)
		s.mu.Lock()
		delete(s.running, sub)
		s.finished = append(s.finished, sub)
		s.mu.Unlock()
	}()
	return sub
}

func (s *SubprocessSet) NextFinished() *Subprocess {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.finished) == 0 {
		return nil
	}
	sub := s.finished[0]
	s.finished = s.finished[1:]
	return sub
}

func (s *SubprocessSet) DoWork() bool {
	// 简单实现：非阻塞检查，返回是否被中断（这里永远返回 false）
	return false
}

func (s *SubprocessSet) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.running {
		sub.cmd.Process.Kill()
		delete(s.running, sub)
	}
	s.finished = nil
}

func (s *Subprocess) Finish() ExitStatus {
	err := <-s.done
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return ExitFailure
		}
		return ExitFailure
	}
	return ExitSuccess
}

func (s *Subprocess) GetOutput() string {
	return s.output
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
	return &RealCommandRunner{
		config:        config,
		jobserver:     jobserver,
		subprocs:      NewSubprocessSet(),
		subprocToEdge: make(map[*Subprocess]*Edge),
	}
}

func (r *RealCommandRunner) CanRunMore() int {
	subprocNumber := len(r.subprocs.running) + len(r.subprocs.finished)
	capacity := r.config.Parallelism - subprocNumber
	if r.jobserver != nil {
		capacity = int(^uint(0) >> 1) // 相当于 INT_MAX
	}
	if r.config.MaxLoadAverage > 0 {
		load := util.GetLoadAverage()
		loadCapacity := int(r.config.MaxLoadAverage - load)
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
	subproc := r.subprocs.Add(command, edge.UseConsole())
	if subproc == nil {
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
			r.jobserver.Release(edge.jobSlot)
		}
	}
}
