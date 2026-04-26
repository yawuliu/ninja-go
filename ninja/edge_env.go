package main

import (
	"slices"
	"strings"
)

type EscapeKind int

const (
	kShellEscape EscapeKind = iota
	kDoNotEscape
)

// EdgeEnv 实现 Env 接口，用于展开 $in、$out 等
type EdgeEnv struct {
	edge_          *Edge
	escape_in_out_ EscapeKind
	recursive_     bool
	lookups_       []string
}

func NewEdgeEnv(edge *Edge, escape EscapeKind) *EdgeEnv {
	e := new(EdgeEnv)
	e.edge_ = edge
	e.escape_in_out_ = escape
	e.recursive_ = false
	return e
}

func (e *EdgeEnv) LookupVariable(varName string) string {
	switch varName {
	case "in", "in_newline":
		sep := byte(' ')
		if varName == "in" {
			sep = ' '
		} else {
			sep = '\n'
		}
		explicit_deps_count := len(e.edge_.inputs_) - e.edge_.implicit_deps_ - e.edge_.order_only_deps_
		return e.makePathList(e.edge_.inputs_[:explicit_deps_count], sep)
	case "out":
		explicit_outs_count := len(e.edge_.outputs_) - e.edge_.implicit_outs_
		return e.makePathList(e.edge_.outputs_[:explicit_outs_count], ' ')
	}

	// 处理递归变量检测
	if e.recursive_ {
		if idx := slices.Index(e.lookups_, varName); idx != -1 {
			var cycle strings.Builder
			for _, v := range e.lookups_[idx:] {
				cycle.WriteString(v)
				cycle.WriteString(" -> ")
			}
			cycle.WriteString(varName)
			panic("cycle in rule variables: " + cycle.String())
		}
	}

	eval := e.edge_.rule_.GetBinding(varName)
	record_varname := e.recursive_ && eval != nil
	if record_varname {
		e.lookups_ = append(e.lookups_, varName)
	}

	e.recursive_ = true
	var result string
	if e.edge_.env_ != nil {
		result = e.edge_.env_.LookupWithFallback(varName, eval, e)
	}

	if record_varname {
		e.lookups_ = e.lookups_[:len(e.lookups_)-1]
	}
	return result
}

func (e *EdgeEnv) makePathList(nodes []*Node, sep byte) string {
	var parts []string
	for _, n := range nodes {
		path := n.path_ // 实际应使用 PathDecanonicalized
		if e.escape_in_out_ == kShellEscape {
			// 需要 shell 转义，这里调用 util.EscapeShell
			if IsWindows() {
				parts = append(parts, GetWin32EscapedString(path))
			} else {
				parts = append(parts, GetShellEscapedString(path))
			}
		} else {
			parts = append(parts, path)
		}
	}
	return strings.Join(parts, string(sep))
}
