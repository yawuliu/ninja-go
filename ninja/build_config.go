package main

type Verbosity int

// BuildConfig 构建配置
type BuildConfig struct {
	verbosity                Verbosity
	dry_run                  bool
	parallelism              int
	disable_jobserver_client bool
	failures_allowed         int
	max_load_average         float64
	depfile_parser_options   *DepfileParserOptions
}

func (b *BuildConfig) GetVerbosity() Verbosity          { return b.verbosity }
func (b *BuildConfig) SetVerbosity(verbosity Verbosity) { b.verbosity = verbosity }
func (b *BuildConfig) GetDryRun() bool                  { return b.dry_run }
func (b *BuildConfig) SetDryRun(dryRun bool)            { b.dry_run = dryRun }
func (b *BuildConfig) GetParallelism() int              { return b.parallelism }
func (b *BuildConfig) SetParallelism(parallelism int)   { b.parallelism = parallelism }
func (b *BuildConfig) GetDisableJobserverClient() bool  { return b.disable_jobserver_client }
func (b *BuildConfig) SetDisableJobserverClient(disableJobserverClient bool) {
	b.disable_jobserver_client = disableJobserverClient
}
func (b *BuildConfig) GetFailuresAllowed() int                  { return b.failures_allowed }
func (b *BuildConfig) SetFailuresAllowed(failuresAllowed int)   { b.failures_allowed = failuresAllowed }
func (b *BuildConfig) GetMaxLoadAverage() float64               { return b.max_load_average }
func (b *BuildConfig) SetMaxLoadAverage(maxLoadAverage float64) { b.max_load_average = maxLoadAverage }

// Verbosity 等级定义
const (
	QUIET Verbosity = iota
	NO_STATUS_UPDATE
	NORMAL
	VERBOSE
)

// DefaultBuildConfig 返回默认的构建配置
func DefaultBuildConfig() BuildConfig {
	return BuildConfig{
		verbosity:                NORMAL,
		dry_run:                  false,
		parallelism:              1,
		disable_jobserver_client: false,
		failures_allowed:         1,
		max_load_average:         -0.0,
		depfile_parser_options:   &DepfileParserOptions{}, // 使用默认值
	}
}
