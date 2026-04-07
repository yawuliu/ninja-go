package builder

type Verbosity int

// BuildConfig 构建配置
type BuildConfig struct {
	Verbosity              Verbosity
	DryRun                 bool
	Parallelism            int
	FailuresAllowed        int
	DisableJobserverClient bool
	MaxLoadAverage         float64
	DepfileParserOptions   *DepfileParserOptions
}

// Verbosity 等级定义
const (
	VerbosityQuiet Verbosity = iota
	VerbosityNoStatusUpdate
	VerbosityNormal
	VerbosityVerbose
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
