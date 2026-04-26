package main

// CommandCollector 收集从给定节点可达的所有构建边（排除 phony 边）。
// 保证结果切片中边的顺序与依赖关系一致（输入边在前，输出边在后）。
type CommandCollector struct {
	visitedNodes map[*Node]bool
	visitedEdges map[*Edge]bool
	inEdges      []*Edge // 收集到的边，按遍历顺序
}

// NewCommandCollector 创建新的收集器实例。
func NewCommandCollector() *CommandCollector {
	return &CommandCollector{
		visitedNodes: make(map[*Node]bool),
		visitedEdges: make(map[*Edge]bool),
		inEdges:      []*Edge{},
	}
}
func (c *CommandCollector) InEdges() []*Edge {
	return c.inEdges
}

// CollectFrom 从指定节点开始递归收集所有相关的边。
func (c *CommandCollector) CollectFrom(node *Node) {
	if node == nil {
		return
	}
	if c.visitedNodes[node] {
		return
	}
	c.visitedNodes[node] = true

	edge := node.in_edge()
	if edge == nil {
		return
	}
	if c.visitedEdges[edge] {
		return
	}
	c.visitedEdges[edge] = true

	// 先递归处理输入节点
	for _, inputNode := range edge.inputs_ {
		c.CollectFrom(inputNode)
	}

	// 如果不是 phony 边，则添加到结果中
	if !edge.IsPhony() {
		c.inEdges = append(c.inEdges, edge)
	}
}
