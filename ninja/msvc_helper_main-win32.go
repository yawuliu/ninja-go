// Package msvc 提供 MSVC 编译辅助工具。
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CLWrapper 封装 cl.exe 命令执行。
type CLWrapper struct {
	envBlock []byte // Windows 环境块（可选）
}

// SetEnvBlock 设置环境块（格式为 null 分隔的 key=value 对，末尾双 null）。
func (c *CLWrapper) SetEnvBlock(envBlock []byte) {
	c.envBlock = envBlock
}

// Run 执行命令并捕获输出。
func (c *CLWrapper) Run(command string) (string, int) {
	cmd := exec.Command("cl.exe", strings.Fields(command)...)
	if c.envBlock != nil {
		// 解析环境块为 map
		env := make([]string, 0)
		envMap := make(map[string]string)
		// 获取当前环境
		for _, e := range os.Environ() {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envMap[parts[0]] = parts[1]
			}
		}
		// 应用环境块中的变量
		data := c.envBlock
		for i := 0; i < len(data); {
			if data[i] == 0 {
				break
			}
			end := i
			for end < len(data) && data[end] != 0 {
				end++
			}
			pair := string(data[i:end])
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 {
				envMap[parts[0]] = parts[1]
			}
			i = end + 1
		}
		for k, v := range envMap {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	output := stdout.String() + stderr.String()
	return output, exitCode
}

// EscapeForDepfile 转义路径中的空格和反斜杠，用于 depfile 格式。
func EscapeForDepfile(path string) string {
	// 简单转义：将反斜杠和空格替换为带反斜杠的版本
	// 注意：depfile 格式需要将空格转义为 "\ "，反斜杠本身也需要转义。
	var result strings.Builder
	for _, ch := range path {
		switch ch {
		case ' ':
			result.WriteString("\\ ")
		case '\\':
			result.WriteString("\\\\")
		default:
			result.WriteRune(ch)
		}
	}
	return result.String()
}

// WriteDepFile 将依赖信息写入 .d 文件。
func WriteDepFile(objectPath string, includes []string) error {
	depfilePath := objectPath + ".d"
	f, err := os.Create(depfilePath)
	if err != nil {
		os.Remove(objectPath)
		return fmt.Errorf("opening %s: %v", depfilePath, err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s: ", objectPath); err != nil {
		os.Remove(objectPath)
		f.Close()
		os.Remove(depfilePath)
		return fmt.Errorf("writing %s", depfilePath)
	}
	for _, inc := range includes {
		escaped := EscapeForDepfile(inc)
		if _, err := fmt.Fprintf(f, "%s\n", escaped); err != nil {
			os.Remove(objectPath)
			f.Close()
			os.Remove(depfilePath)
			return fmt.Errorf("writing %s", depfilePath)
		}
	}
	return nil
}

// MSVCHelperMain 是 msvc 工具的入口点。
func MSVCHelperMain(args []string) int {
	// 解析选项
	var envFile, outputFile, depsPrefix string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			Usage()
			return 0
		case "-e":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "missing argument for -e")
				return 1
			}
			envFile = args[i+1]
			i++
		case "-o":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "missing argument for -o")
				return 1
			}
			outputFile = args[i+1]
			i++
		case "-p":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "missing argument for -p")
				return 1
			}
			depsPrefix = args[i+1]
			i++
		default:
			// 剩余参数是命令（应该包含 -- 分隔符）
		}
	}

	// 查找 " -- " 分隔符
	var command string
	cmdArgs := args
	for i := 0; i < len(cmdArgs); i++ {
		if cmdArgs[i] == "--" {
			if i+1 >= len(cmdArgs) {
				fmt.Fprintln(os.Stderr, "expected command after --")
				return 1
			}
			command = strings.Join(cmdArgs[i+1:], " ")
			break
		}
	}
	if command == "" {
		fmt.Fprintln(os.Stderr, "expected command line to end with \" -- command args\"")
		return 1
	}

	// 读取环境文件（如果提供）
	var envBlock []byte
	if envFile != "" {
		data, err := os.ReadFile(envFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "couldn't open %s: %v\n", envFile, err)
			return 1
		}
		envBlock = data
		// 将环境块中的 PATH 注入当前进程
		pushPathIntoEnvironment(envBlock)
	}

	cl := &CLWrapper{}
	if len(envBlock) > 0 {
		cl.SetEnvBlock(envBlock)
	}
	output, exitCode := cl.Run(command)

	if outputFile != "" {
		parser := NewCLParser()
		var err string
		var filtered string
		if !parser.Parse(output, depsPrefix, &filtered, &err) {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		output = filtered
	}

	if output != "" {
		// 以二进制模式输出，避免换行符转换
		os.Stdout.WriteString(output)
	}
	return exitCode
}

// pushPathIntoEnvironment 从环境块中提取 PATH 并设置为当前进程环境。
func pushPathIntoEnvironment(envBlock []byte) {
	data := envBlock
	for i := 0; i < len(data); {
		if data[i] == 0 {
			break
		}
		end := i
		for end < len(data) && data[end] != 0 {
			end++
		}
		pair := string(data[i:end])
		if len(pair) > 5 && strings.ToLower(pair[:5]) == "path=" {
			os.Setenv("PATH", pair[5:])
			return
		}
		i = end + 1
	}
}

// Usage 打印帮助信息。
func Usage() {
	fmt.Println(`usage: ninja -t msvc [options] -- cl.exe /showIncludes /otherArgs
options:
  -e ENVFILE load environment block from ENVFILE as environment
  -o FILE    write output dependency information to FILE.d
  -p STRING  localized prefix of msvc's /showIncludes output`)
}
