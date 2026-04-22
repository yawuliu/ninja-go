package main

import (
	"fmt"
	"ninja-go/ninja/util"
	"os"
	"runtime"
	"strings"
)

// Status 状态显示接口（可简化）

// Status 接口
type Status interface {
	EdgeAddedToPlan(edge *Edge)
	EdgeRemovedFromPlan(edge *Edge)
	BuildEdgeStarted(edge *Edge, startTimeMillis int64)
	BuildEdgeFinished(edge *Edge, startTimeMillis, endTimeMillis int64, exitCode ExitStatus, output string)
	BuildStarted()
	BuildFinished()
	SetExplanations(expl *Explanations)
	Info(msg string, args ...interface{})
	Warning(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// StatusPrinter 默认的控制台输出实现。
type StatusPrinter struct {
	config_       BuildConfig
	explanations_ *Explanations

	started_edges_  int
	finished_edges_ int
	total_edges_    int
	running_edges_  int

	/// How much wall clock elapsed so far?
	time_millis_ int64

	/// How much cpu clock elapsed so far?
	cpu_time_millis_ int64

	/// What percentage of predicted total time have elapsed already?
	time_predicted_percentage_ float64

	/// Out of all the edges, for how many do we know previous time?
	eta_predictable_edges_total_ int
	/// And how much time did they all take?
	eta_predictable_cpu_time_total_millis_ int64

	/// Out of all the non-finished edges, for how many do we know previous time?
	eta_predictable_edges_remaining_ int
	/// And how much time will they all take?
	eta_predictable_cpu_time_remaining_millis_ int64

	/// For how many edges we don't know the previous run time?
	eta_unpredictable_edges_remaining_ int

	/// The custom progress status format to use.
	progress_status_format_ string

	current_rate_ *SlidingRateInfo

	/// Prints progress output.
	printer_ LinePrinter
}

type SlidingRateInfo struct {
	rate       float64
	N          int
	times      []int64
	lastUpdate int
}

func NewSlidingRateInfo(n int) *SlidingRateInfo {
	return &SlidingRateInfo{
		rate:       -1,
		N:          n,
		times:      make([]int64, 0, n),
		lastUpdate: -1,
	}
}

func (s *SlidingRateInfo) Rate() float64 {
	return s.rate
}

func (s *SlidingRateInfo) UpdateRate(updateHint int, timeMillis int64) {
	if updateHint == s.lastUpdate {
		return
	}
	s.lastUpdate = updateHint

	s.times = append(s.times, timeMillis)
	if len(s.times) > s.N {
		s.times = s.times[1:]
	}

	if len(s.times) < 2 {
		return
	}
	front := s.times[0]
	back := s.times[len(s.times)-1]
	if back != front {
		durationSec := float64(back-front) / 1000.0
		s.rate = float64(len(s.times)) / durationSec
	}
}

// NewStatus 根据 BuildConfig 创建 Status 实例。
func NewStatusPrinter(config BuildConfig) *StatusPrinter {
	s := &StatusPrinter{}
	s.config_ = config
	s.started_edges_ = 0
	s.finished_edges_ = 0
	s.total_edges_ = 0
	s.running_edges_ = 0
	s.progress_status_format_ = ""
	s.current_rate_ = NewSlidingRateInfo(config.parallelism)
	// Don't do anything fancy in verbose mode.
	if s.config_.verbosity != NORMAL {
		s.printer_.set_smart_terminal(false)
	}
	s.progress_status_format_ = os.Getenv("NINJA_STATUS")
	if s.progress_status_format_ == "" {
		s.progress_status_format_ = "[%f/%t] "
	}
	return s
}

func (sp *StatusPrinter) EdgeAddedToPlan(edge *Edge) {
	sp.total_edges_++

	// Do we know how long did this edge take last time?
	if edge.prev_elapsed_time_millis != -1 {
		sp.eta_predictable_edges_total_++
		sp.eta_predictable_edges_remaining_++
		sp.eta_predictable_cpu_time_total_millis_ += edge.prev_elapsed_time_millis
		sp.eta_predictable_cpu_time_remaining_millis_ += edge.prev_elapsed_time_millis
	} else {
		sp.eta_unpredictable_edges_remaining_++
	}
}

func (sp *StatusPrinter) EdgeRemovedFromPlan(edge *Edge) {
	sp.total_edges_--

	// Do we know how long did this edge take last time?
	if edge.prev_elapsed_time_millis != -1 {
		sp.eta_predictable_edges_total_--
		sp.eta_predictable_edges_remaining_--
		sp.eta_predictable_cpu_time_total_millis_ -= edge.prev_elapsed_time_millis
		sp.eta_predictable_cpu_time_remaining_millis_ -= edge.prev_elapsed_time_millis
	} else {
		sp.eta_unpredictable_edges_remaining_--
	}
}

func (sp *StatusPrinter) BuildEdgeStarted(edge *Edge, start_time_millis int64) {
	sp.started_edges_++
	sp.running_edges_++
	sp.time_millis_ = start_time_millis

	if edge.use_console() || sp.printer_.is_smart_terminal() {
		sp.PrintStatus(edge, start_time_millis)
	}

	if edge.use_console() {
		sp.printer_.SetConsoleLocked(true)
	}
}

func (sp *StatusPrinter) BuildEdgeFinished(edge *Edge, startTimeMillis int64, endTimeMillis int64, exitCode ExitStatus, output string) {
	sp.time_millis_ = endTimeMillis
	sp.finished_edges_++

	elapsed := endTimeMillis - startTimeMillis
	sp.cpu_time_millis_ += elapsed

	// Do we know how long did this edge take last time?
	if edge.prev_elapsed_time_millis != -1 {
		sp.eta_predictable_edges_remaining_--
		sp.eta_predictable_cpu_time_remaining_millis_ -= edge.prev_elapsed_time_millis
	} else {
		sp.eta_unpredictable_edges_remaining_--
	}

	if edge.use_console() {
		sp.printer_.SetConsoleLocked(false)
	}

	if sp.config_.verbosity == QUIET {
		return
	}

	if !edge.use_console() {
		sp.PrintStatus(edge, endTimeMillis)
	}

	sp.running_edges_--

	// Print the command that is spewing before printing its output.
	if exitCode != ExitSuccess {
		var outputs string
		for _, o := range edge.outputs_ {
			outputs += o.Path + " "
		}

		failed := "FAILED: [code=" + fmt.Sprint(exitCode) + "] "
		if sp.printer_.supports_color() {
			sp.printer_.PrintOnNewLine("\x1B[31m" + failed + "\x1B[0m" + outputs + "\n")
		} else {
			sp.printer_.PrintOnNewLine(failed + outputs + "\n")
		}
		sp.printer_.PrintOnNewLine(edge.EvaluateCommand(false) + "\n")
	}

	if output != "" {
		if runtime.GOOS == "windows" {
			// Fix extra CR being added on Windows, writing out CR CR LF (#773)
			os.Stdout.Sync()
			// In Go, we can't directly change mode of stdout to binary,
			// but we can use syscall.SetStdHandle? For simplicity, we assume
			// that the output is already correct; the original behavior is complex.
			// We'll just write as is.
		}

		// ninja sets stdout and stderr of subprocesses to a pipe, to be able to
		// check if the output is empty. Some compilers, e.g. clang, check
		// isatty(stderr) to decide if they should print colored output.
		// To make it possible to use colored output with ninja, subprocesses should
		// be run with a flag that forces them to always print color escape codes.
		// To make sure these escape codes don't show up in a file if ninja's output
		// is piped to a file, ninja strips ansi escape codes again if it's not
		// writing to a |smart_terminal_|.
		if sp.printer_.supports_color() || !strings.Contains(output, "\x1b") {
			sp.printer_.PrintOnNewLine(output)
		} else {
			finalOutput := util.StripAnsiEscapeCodes(output)
			sp.printer_.PrintOnNewLine(finalOutput)
		}

		if runtime.GOOS == "windows" {
			os.Stdout.Sync()
			// Restore text mode is not needed in Go.
		}
	}
}

func (s *StatusPrinter) BuildStarted() {
	s.started_edges_ = 0
	s.finished_edges_ = 0
	s.running_edges_ = 0
}

func (s *StatusPrinter) BuildFinished() {
	s.printer_.SetConsoleLocked(false)
	s.printer_.PrintOnNewLine("")
}

func (s *StatusPrinter) SetExplanations(expl *Explanations) {
	s.explanations_ = expl
}

func (s *StatusPrinter) Warning(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	Warning(formatted) // 假设全局函数 Warning(string) 存在
}

func (s *StatusPrinter) Error(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	Error(formatted) // 假设全局函数 Error(string) 存在
}
func (s *StatusPrinter) Info(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	Info(formatted) // 假设全局函数 Info(string) 存在
}

func Warning(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ninja: warning: "+msg+"\n", args...)
}

func Error(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ninja: error: "+msg+"\n", args...)
}

func Info(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, "ninja: "+msg+"\n", args...)
}

// 辅助变量，用于测试时可重定向
var Stderr = ConsoleStderr()

type consoleStderr struct{}

func (c consoleStderr) Write(p []byte) (n int, err error) {
	return os.Stderr.Write(p)
}

func ConsoleStderr() *consoleStderr { return &consoleStderr{} }

func (sp *StatusPrinter) PrintStatus(edge *Edge, timeMillis int64) {
	if sp.explanations_ != nil {
		var explanations []string
		for _, output := range edge.outputs_ {
			sp.explanations_.LookupAndAppend(output, &explanations)
		}
		if len(explanations) > 0 {
			sp.printer_.PrintOnNewLine("")
			for _, exp := range explanations {
				fmt.Fprintf(os.Stderr, "ninja explain: %s\n", exp)
			}
		}
	}

	if sp.config_.verbosity == QUIET || sp.config_.verbosity == NO_STATUS_UPDATE {
		return
	}

	sp.RecalculateProgressPrediction()

	forceFullCommand := sp.config_.verbosity == VERBOSE

	toPrint := edge.GetBinding("description")
	if toPrint == "" || forceFullCommand {
		toPrint = edge.GetBinding("command")
	}

	toPrint = sp.FormatProgressStatus(sp.progress_status_format_, timeMillis) + toPrint

	lineType := ELIDE
	if forceFullCommand {
		lineType = FULL
	}
	sp.printer_.Print(toPrint, lineType)
}

func (sp *StatusPrinter) RecalculateProgressPrediction() {
	sp.time_predicted_percentage_ = 0.0

	// Sometimes, the previous and actual times may be wildly different.
	// For example, the previous build may have been fully recovered from ccache,
	// so it was blazing fast, while the new build no longer gets hits from ccache
	// for whatever reason, so it actually compiles code, which takes much longer.
	// We should detect such cases, and avoid using "wrong" previous times.

	// Note that we will only use the previous times if there are edges with
	// previous time knowledge remaining.
	usePreviousTimes := sp.eta_predictable_edges_remaining_ != 0 &&
		sp.eta_predictable_cpu_time_remaining_millis_ != 0

	// Iff we have sufficient statistical information for the current run,
	// that is, if we have took at least 15 sec AND finished at least 5% of edges,
	// we can check whether our performance so far matches the previous one.
	if usePreviousTimes && sp.total_edges_ != 0 && sp.finished_edges_ != 0 &&
		(sp.time_millis_ >= 15*1000) &&
		(float64(sp.finished_edges_)/float64(sp.total_edges_) >= 0.05) {
		// Over the edges we've just run, how long did they take on average?
		actualAverageCpuTimeMillis := float64(sp.cpu_time_millis_) / float64(sp.finished_edges_)
		// What is the previous average, for the edges with such knowledge?
		previousAverageCpuTimeMillis := float64(sp.eta_predictable_cpu_time_total_millis_) /
			float64(sp.eta_predictable_edges_total_)

		ratio := max(previousAverageCpuTimeMillis, actualAverageCpuTimeMillis) /
			min(previousAverageCpuTimeMillis, actualAverageCpuTimeMillis)

		// Let's say that the average times should differ by less than 10x
		usePreviousTimes = ratio < 10
	}

	edgesWithKnownRuntime := sp.finished_edges_
	if usePreviousTimes {
		edgesWithKnownRuntime += sp.eta_predictable_edges_remaining_
	}
	if edgesWithKnownRuntime == 0 {
		return
	}

	edgesWithUnknownRuntime := 0
	if usePreviousTimes {
		edgesWithUnknownRuntime = sp.eta_unpredictable_edges_remaining_
	} else {
		edgesWithUnknownRuntime = sp.total_edges_ - sp.finished_edges_
	}

	// Given the time elapsed on the edges we've just run,
	// and the runtime of the edges for which we know previous runtime,
	// what's the edge's average runtime?
	edgesKnownRuntimeTotalMillis := sp.cpu_time_millis_
	if usePreviousTimes {
		edgesKnownRuntimeTotalMillis += sp.eta_predictable_cpu_time_remaining_millis_
	}

	averageCpuTimeMillis := float64(edgesKnownRuntimeTotalMillis) / float64(edgesWithKnownRuntime)

	// For the edges for which we do not have the previous runtime,
	// let's assume that their average runtime is the same as for the other edges,
	// and we therefore can predict their remaining runtime.
	unpredictableCpuTimeRemainingMillis := averageCpuTimeMillis * float64(edgesWithUnknownRuntime)

	// And therefore we can predict the remaining and total runtimes.
	totalCpuTimeRemainingMillis := unpredictableCpuTimeRemainingMillis
	if usePreviousTimes {
		totalCpuTimeRemainingMillis += float64(sp.eta_predictable_cpu_time_remaining_millis_)
	}
	totalCpuTimeMillis := float64(sp.cpu_time_millis_) + totalCpuTimeRemainingMillis
	if totalCpuTimeMillis == 0.0 {
		return
	}

	// After that we can tell how much work we've completed, in time units.
	sp.time_predicted_percentage_ = float64(sp.cpu_time_millis_) / totalCpuTimeMillis
}

func (sp *StatusPrinter) FormatProgressStatus(progressStatusFormat string, timeMillis int64) string {
	var out strings.Builder
	buf := make([]byte, 32)

	for i := 0; i < len(progressStatusFormat); i++ {
		c := progressStatusFormat[i]
		if c != '%' {
			out.WriteByte(c)
			continue
		}

		i++ // skip '%'
		if i >= len(progressStatusFormat) {
			break
		}

		switch progressStatusFormat[i] {
		case '%':
			out.WriteByte('%')

		// Started edges.
		case 's':
			fmt.Fprintf(&out, "%d", sp.started_edges_)

		// Total edges.
		case 't':
			fmt.Fprintf(&out, "%d", sp.total_edges_)

		// Running edges.
		case 'r':
			fmt.Fprintf(&out, "%d", sp.running_edges_)

		// Unstarted edges.
		case 'u':
			fmt.Fprintf(&out, "%d", sp.total_edges_-sp.started_edges_)

		// Finished edges.
		case 'f':
			fmt.Fprintf(&out, "%d", sp.finished_edges_)

		// Overall finished edges per second.
		case 'o':
			rate := float64(sp.finished_edges_) / (float64(sp.time_millis_) / 1000.0)
			sp.snprintfRate(rate, buf, "%.1f")
			out.Write(buf)

		// Current rate, average over the last '-j' jobs.
		case 'c':
			sp.current_rate_.UpdateRate(sp.finished_edges_, sp.time_millis_)
			sp.snprintfRate(sp.current_rate_.Rate(), buf, "%.1f")
			out.Write(buf)

		// Percentage of edges completed
		case 'p':
			percent := 0
			if sp.finished_edges_ != 0 && sp.total_edges_ != 0 {
				percent = (100 * sp.finished_edges_) / sp.total_edges_
			}
			fmt.Fprintf(&out, "%3d%%", percent)

		// Wall time
		case 'e', 'w', 'E', 'W':
			elapsedSec := float64(timeMillis) / 1000.0
			etaSec := -1.0
			if sp.time_predicted_percentage_ != 0.0 {
				totalWallTime := float64(timeMillis) / sp.time_predicted_percentage_
				etaSec = (totalWallTime - float64(timeMillis)) / 1000.0
			}

			printWithHours := elapsedSec >= 60*60 || etaSec >= 60*60

			var sec float64
			switch progressStatusFormat[i] {
			case 'e', 'w':
				sec = elapsedSec
			case 'E', 'W':
				sec = etaSec
			}

			if sec < 0 {
				out.WriteString("?")
			} else {
				switch progressStatusFormat[i] {
				case 'e', 'E':
					fmt.Fprintf(&out, "%.3f", sec)
				case 'w', 'W':
					if printWithHours {
						t := int64(sec)
						fmt.Fprintf(&out, "%d:%02d:%02d", t/3600, (t%3600)/60, t%60)
					} else {
						t := int64(sec)
						fmt.Fprintf(&out, "%02d:%02d", t/60, t%60)
					}
				}
			}

		// Percentage of time spent out of the predicted time total
		case 'P':
			fmt.Fprintf(&out, "%3d%%", int(100.0*sp.time_predicted_percentage_))

		default:
			panic(fmt.Sprintf("unknown placeholder '%%%c' in $NINJA_STATUS", progressStatusFormat[i]))
		}
	}

	return out.String()
}

func (sp *StatusPrinter) snprintfRate(rate float64, buf []byte, format string) {
	if rate == -1 {
		copy(buf, "?")
		// Ensure null termination (not strictly needed in Go, but for compatibility)
		if len(buf) > 1 {
			buf[1] = 0
		}
	} else {
		s := fmt.Sprintf(format, rate)
		copy(buf, s)
		// If the formatted string is shorter than buf, the remaining bytes are left as is.
		// In Go, we typically don't need to null-terminate.
	}
}
