package main

import (
	"fmt"
	"ninja-go/ninja/util"
	"strings"
)

// EdgeEnv 实现 Env 接口，用于展开 $in、$out 等
type EdgeEnv struct {
	edge        *Edge
	escapeInOut bool
	recursive   bool
	lookups     []string
}

func (e *EdgeEnv) LookupVariable(varName string) string {
	switch varName {
	case "in", "in_newline":
		sep := byte(' ')
		if varName == "in_newline" {
			sep = '\n'
		}
		explicitCount := len(e.edge.inputs_) - e.edge.implicit_deps_ - e.edge.order_only_deps_
		return e.makePathList(e.edge.inputs_[:explicitCount], sep)
	case "out":
		explicitCount := len(e.edge.outputs_) - e.edge.implicit_outs_
		return e.makePathList(e.edge.outputs_[:explicitCount], ' ')
	}

	// 处理递归变量检测
	if e.recursive {
		for _, v := range e.lookups {
			if v == varName {
				// 循环依赖
				panic(fmt.Sprintf("cycle in rule variables: %s", strings.Join(append(e.lookups, varName), " -> ")))
			}
		}
	}

	eval := e.edge.Rule.GetBinding(varName)
	if eval == nil {
		if e.edge.env_ != nil {
			return e.edge.env_.LookupVariable(varName)
		}
		return ""
	}

	if e.recursive {
		e.lookups = append(e.lookups, varName)
	}
	e.recursive = true
	result := e.edge.env_.LookupWithFallback(varName, eval, e)
	if len(e.lookups) > 0 && e.lookups[len(e.lookups)-1] == varName {
		e.lookups = e.lookups[:len(e.lookups)-1]
	}
	return result
}

func (e *EdgeEnv) makePathList(nodes []*Node, sep byte) string {
	var parts []string
	for _, n := range nodes {
		path := n.Path // 实际应使用 PathDecanonicalized
		if e.escapeInOut {
			// 需要 shell 转义，这里调用 util.EscapeShell
			if util.IsWindows() {
				parts = append(parts, util.GetWin32EscapedString(path))
			} else {
				parts = append(parts, util.GetShellEscapedString(path))
			}
		} else {
			parts = append(parts, path)
		}
	}
	return strings.Join(parts, string(sep))
}
