package builder

import "ninja-go/pkg/util"

// InputsCollector 收集从起始节点可达的所有输入节点。
type InputsCollector struct {
	inputs       []*Node
	visitedNodes map[*Node]bool
}

// NewInputsCollector 创建新的收集器实例。
func NewInputsCollector() *InputsCollector {
	return &InputsCollector{
		visitedNodes: make(map[*Node]bool),
	}
}

// 因此，Go 实现应为：
func (c *InputsCollector) VisitNode(node *Node) {
	edge := node.InEdge
	if edge == nil {
		// 源文件，不添加
		return
	}

	for _, input := range edge.Inputs {
		if c.visitedNodes[input] {
			continue
		}
		c.visitedNodes[input] = true
		c.VisitNode(input)

		inputEdge := input.InEdge
		if !(inputEdge != nil && inputEdge.IsPhony()) {
			c.inputs = append(c.inputs, input)
		}
	}
}

// Inputs 返回收集到的输入节点切片（按访问顺序）。
func (c *InputsCollector) Inputs() []*Node {
	return c.inputs
}

// GetInputsAsStrings 返回输入节点路径的字符串列表，可选择 shell 转义。
func (c *InputsCollector) GetInputsAsStrings(shellEscape bool) []string {
	result := make([]string, len(c.inputs))
	for i, n := range c.inputs {
		if shellEscape {
			result[i] = util.GetShellEscapedString(n.Path)
		} else {
			result[i] = n.Path
		}
	}
	return result
}

// Reset 重置收集器状态。
func (c *InputsCollector) Reset() {
	c.inputs = nil
	c.visitedNodes = make(map[*Node]bool)
}
