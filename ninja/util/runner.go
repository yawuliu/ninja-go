package util

import "io"

// CommandRunner 执行命令的接口
type CommandRunner interface {
	Run(cmdLine string, stdout, stderr io.Writer) error
}
