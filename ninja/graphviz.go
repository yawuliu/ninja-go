package main

import (
	"fmt"
	"os"
	"strings"
)

// GraphViz 生成 GraphViz dot 文件输出。
type GraphViz struct {
	dyndepLoader *DyndepLoader
	visitedNodes map[*Node]bool
	visitedEdges map[*Edge]bool
}

// NewGraphViz 创建 GraphViz 实例。
func NewGraphViz(state *State, diskInterface FileSystem) *GraphViz {
	return &GraphViz{
		dyndepLoader: NewDyndepLoader(state, diskInterface),
		visitedNodes: make(map[*Node]bool),
		visitedEdges: make(map[*Edge]bool),
	}
}

// Start 输出 dot 文件头部。
func (g *GraphViz) Start() {
	fmt.Println("digraph ninja {")
	fmt.Println("rankdir=\"LR\"")
	fmt.Println("node [fontsize=10, shape=box, height=0.25]")
	fmt.Println("edge [fontsize=10]")
}

// AddTarget 递归添加节点及其依赖边到图中。
func (g *GraphViz) AddTarget(node *Node) {
	if g.visitedNodes[node] {
		return
	}

	// 转义路径中的反斜杠为斜杠（用于显示）
	pathStr := strings.ReplaceAll(node.path_, "\\", "/")
	fmt.Printf("\"%p\" [label=\"%s\"]\n", node, pathStr)
	g.visitedNodes[node] = true

	edge := node.in_edge()
	if edge == nil {
		// 叶子节点（源文件），无需绘制边
		return
	}

	if g.visitedEdges[edge] {
		return
	}
	g.visitedEdges[edge] = true

	// 如果边有挂起的 dyndep 文件，尝试加载
	if edge.dyndep_ != nil && edge.dyndep_.dyndep_pending_ {
		var err string
		if !g.dyndepLoader.LoadDyndeps(edge.dyndep_, &err) {
			fmt.Fprintf(os.Stderr, "ninja: warning: %v\n", err)
		}
	}

	// 绘制边
	if len(edge.inputs_) == 1 && len(edge.outputs_) == 1 {
		// 简单单输入单输出边
		fmt.Printf("\"%p\" -> \"%p\" [label=\" %s\"]\n",
			edge.inputs_[0], edge.outputs_[0], edge.Rule.Name)
	} else {
		// 复杂边：创建一个椭圆节点表示规则，然后连接输入和输出
		fmt.Printf("\"%p\" [label=\"%s\", shape=ellipse]\n", edge, edge.Rule.Name)
		for _, out := range edge.outputs_ {
			fmt.Printf("\"%p\" -> \"%p\"\n", edge, out)
		}
		for idx, in := range edge.inputs_ {
			orderOnly := ""
			if edge.IsOrderOnly(idx) {
				orderOnly = " style=dotted"
			}
			fmt.Printf("\"%p\" -> \"%p\" [arrowhead=none%s]\n", in, edge, orderOnly)
		}
	}

	// 递归处理所有输入节点
	for _, in := range edge.inputs_ {
		g.AddTarget(in)
	}
}

// Finish 输出 dot 文件尾部。
func (g *GraphViz) Finish() {
	fmt.Println("}")
}
