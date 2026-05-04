//go:build !windows

package main

import "os/exec"

func makeCmd(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}
