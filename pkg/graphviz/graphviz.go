package graphviz

import (
	"fmt"
	"ninja-go/pkg/builder"
	"ninja-go/pkg/util"
	"os"
	"strings"
)

// GraphViz 生成 GraphViz dot 文件输出。
type GraphViz struct {
	dyndepLoader *builder.DyndepLoader
	visitedNodes map[*builder.Node]bool
	visitedEdges map[*builder.Edge]bool
}

// NewGraphViz 创建 GraphViz 实例。
func NewGraphViz(state *builder.State, diskInterface util.FileSystem) *GraphViz {
	return &GraphViz{
		dyndepLoader: builder.NewDyndepLoader(state, diskInterface),
		visitedNodes: make(map[*builder.Node]bool),
		visitedEdges: make(map[*builder.Edge]bool),
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
func (g *GraphViz) AddTarget(node *builder.Node) {
	if g.visitedNodes[node] {
		return
	}

	// 转义路径中的反斜杠为斜杠（用于显示）
	pathStr := strings.ReplaceAll(node.Path, "\\", "/")
	fmt.Printf("\"%p\" [label=\"%s\"]\n", node, pathStr)
	g.visitedNodes[node] = true

	edge := node.InEdge
	if edge == nil {
		// 叶子节点（源文件），无需绘制边
		return
	}

	if g.visitedEdges[edge] {
		return
	}
	g.visitedEdges[edge] = true

	// 如果边有挂起的 dyndep 文件，尝试加载
	if edge.DyndepFile != nil && edge.DyndepFile.DyndepPending {
		if err := g.dyndepLoader.LoadDyndeps(edge.DyndepFile); err != nil {
			fmt.Fprintf(os.Stderr, "ninja: warning: %v\n", err)
		}
	}

	// 绘制边
	if len(edge.Inputs) == 1 && len(edge.Outputs) == 1 {
		// 简单单输入单输出边
		fmt.Printf("\"%p\" -> \"%p\" [label=\" %s\"]\n",
			edge.Inputs[0], edge.Outputs[0], edge.Rule.Name)
	} else {
		// 复杂边：创建一个椭圆节点表示规则，然后连接输入和输出
		fmt.Printf("\"%p\" [label=\"%s\", shape=ellipse]\n", edge, edge.Rule.Name)
		for _, out := range edge.Outputs {
			fmt.Printf("\"%p\" -> \"%p\"\n", edge, out)
		}
		for idx, in := range edge.Inputs {
			orderOnly := ""
			if edge.IsOrderOnly(idx) {
				orderOnly = " style=dotted"
			}
			fmt.Printf("\"%p\" -> \"%p\" [arrowhead=none%s]\n", in, edge, orderOnly)
		}
	}

	// 递归处理所有输入节点
	for _, in := range edge.Inputs {
		g.AddTarget(in)
	}
}

// Finish 输出 dot 文件尾部。
func (g *GraphViz) Finish() {
	fmt.Println("}")
}
