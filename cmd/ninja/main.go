package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"ninja-go/pkg/builder"
	"ninja-go/pkg/graph"
	"ninja-go/pkg/parser"
)

func main() {
	// 命令行标志
	buildFile := flag.String("f", "build.ninja", "input build file")
	parallel := flag.Int("j", runtime.NumCPU(), "number of parallel jobs")
	flag.Parse()
	targets := flag.Args() // 剩余参数作为要构建的目标

	if len(targets) == 0 {
		// 默认目标: 从 build.ninja 中解析 default 语句得到
		targets = []string{"default"}
	}

	// 1. 解析 .ninja 文件
	state := graph.NewState()
	p := parser.NewParser(state)
	if err := p.ParseFile(*buildFile); err != nil {
		fmt.Fprintf(os.Stderr, "ninja: parse error: %v\n", err)
		os.Exit(1)
	}

	// 2. 加载持久化日志（.ninja_log, .ninja_deps）
	// TODO: 加载日志

	// 3. 构建计划
	b := builder.NewBuilder(state, *parallel, nil, nil)
	if err := b.Build(targets); err != nil {
		fmt.Fprintf(os.Stderr, "ninja: build error: %v\n", err)
		os.Exit(1)
	}
}
