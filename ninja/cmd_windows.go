//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func makeCmd(command string) *exec.Cmd {
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: "cmd /c " + command,
	}
	return cmd
}
