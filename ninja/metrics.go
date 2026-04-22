package main

import (
	"fmt"
	"sync"
	"time"
)

// Metric 单个统计指标
type Metric struct {
	Name  string
	Count int
	Sum   int64 // 累计时间（纳秒）
}

// Metrics 指标收集器
type Metrics struct {
	mu      sync.Mutex
	metrics []*Metric
}

// NewMetric 创建或返回已存在的同名指标
func NewMetric(name string) *Metric {
	m := &Metrics{}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, met := range m.metrics {
		if met.Name == name {
			return met
		}
	}
	met := &Metric{Name: name}
	m.metrics = append(m.metrics, met)
	return met
}

// Report 打印统计报告
func (m *Metrics) Report() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.metrics) == 0 {
		return
	}
	// 计算列宽
	width := 0
	for _, met := range m.metrics {
		if len(met.Name) > width {
			width = len(met.Name)
		}
	}
	fmt.Printf("%-*s\t%-6s\t%-9s\t%s\n", width, "metric", "count", "avg (us)", "total (ms)")
	for _, met := range m.metrics {
		if met.Count == 0 {
			continue
		}
		// sum 是纳秒，转换为微秒
		micros := met.Sum / 1000
		totalMs := float64(micros) / 1000.0
		avg := float64(micros) / float64(met.Count)
		fmt.Printf("%-*s\t%-6d\t%-8.1f\t%.1f\n", width, met.Name, met.Count, avg, totalMs)
	}
}

// ScopedMetric 用于在函数内记录耗时（配合 defer 使用）
type ScopedMetric struct {
	metric *Metric
	start  int64
}

// NewScopedMetric 创建并开始计时，如果 metric 为 nil 则无效
func NewScopedMetric(metric *Metric) *ScopedMetric {
	if metric == nil {
		return nil
	}
	return &ScopedMetric{
		metric: metric,
		start:  time.Now().UnixNano(),
	}
}

// Close 结束计时并累加到指标中（通常在 defer 中调用）
func (s *ScopedMetric) Close() {
	if s == nil || s.metric == nil {
		return
	}
	dt := time.Now().UnixNano() - s.start
	s.metric.Count++
	s.metric.Sum += dt
}
func HighResTimer() int64 {
	return time.Now().UnixNano()
}

func TimerToMicros(dt int64) int64 {
	return time.Duration(dt).Microseconds()
}

// GetTimeMillis 返回当前时间毫秒数（自 Unix 纪元）
func GetTimeMillis() int64 {
	return time.Now().UnixMilli()
}

// Stopwatch 简单秒表
type Stopwatch struct {
	started int64
}

// Restart 重置秒表
func (s *Stopwatch) Restart() {
	s.started = time.Now().UnixNano()
}

// Elapsed 返回自上次 Restart 以来的秒数
func (s *Stopwatch) Elapsed() float64 {
	dt := time.Now().UnixNano() - s.started
	return float64(dt) / 1e9
}

// 全局指标实例（导出为包级变量，对应 C++ 的 g_metrics）
var gMetrics *Metrics

func init() {
	// 可选：初始化全局指标，但实际由外部调用设置
}

// SetMetrics 设置全局指标实例
func SetMetrics(m *Metrics) {
	gMetrics = m
}

// GetMetrics 获取全局指标实例
func GetMetrics() *Metrics {
	return gMetrics
}

// 辅助宏：如果 gMetrics 非空，则记录指标
// 由于 Go 没有宏，用户可手动调用：
//   if metrics.GetMetrics() != nil {
//       m := metrics.GetMetrics().NewMetric("name")
//       defer metrics.NewScopedMetric(m).Close()
//   }
