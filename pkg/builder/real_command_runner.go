package builder

import (
	"io"
	"ninja-go/pkg/util"
)

// RealCommandRunner 真实命令执行器
type RealCommandRunner struct{}

func NewRealCommandRunner() *RealCommandRunner {
	return &RealCommandRunner{}
}
func (r *RealCommandRunner) Run(cmdLine string, stdout, stderr io.Writer) error {
	cmd := util.CommandForShell(cmdLine)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
