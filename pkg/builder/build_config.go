package builder

// BuildConfig 构建配置
type BuildConfig struct {
	Verbosity              int
	DryRun                 bool
	Parallelism            int
	FailuresAllowed        int
	DisableJobserverClient bool
	MaxLoadAverage         float64
	DepfileParserOptions   *DepfileParserOptions
}

type Verbosity int

const (
	Quiet Verbosity = iota
	NoStatusUpdate
	Normal
	Verbose
)

// DefaultBuildConfig 返回默认的构建配置
func DefaultBuildConfig() BuildConfig {
	return BuildConfig{
		Verbosity:              VerbosityNormal,
		DryRun:                 false,
		Parallelism:            1,
		DisableJobserverClient: false,
		FailuresAllowed:        1,
		MaxLoadAverage:         -0.0,
		DepfileParserOptions:   &DepfileParserOptions{}, // 使用默认值
	}
}
