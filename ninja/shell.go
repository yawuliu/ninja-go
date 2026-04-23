package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// CommandForShell 根据操作系统返回合适的命令执行器
func CommandForShell(cmdLine string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		// Windows 使用 cmd /c，注意转义双引号等
		// 简单处理：整个命令作为参数传给 /c
		wrapped := fmt.Sprintf("chcp 65001 > nul && %s", cmdLine)
		return exec.Command("cmd", "/c", wrapped)
	}

	return exec.Command("sh", "-c", cmdLine)
}

// ToNativePath 将内部正斜杠路径转换为操作系统原生路径
func ToNativePath(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(path, "/", "\\")
	}
	return path
}

// WriteResponseFile 生成响应文件内容（将参数列表写入文件）
func WriteResponseFile(path string, args []string) error {
	// 每个参数一行，或者空格分隔（需要转义包含空格的参数）
	content := strings.Join(args, "\n")
	return os.WriteFile(path, []byte(content), 0644)
}
