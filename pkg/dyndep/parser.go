package dyndep

import (
	"bufio"
	"fmt"
	"strings"

	"ninja-go/pkg/graph"
	"ninja-go/pkg/util"
)

// DyndepParser 解析 .dd 文件
type DyndepParser struct {
	state  *graph.State
	scope  map[string]string
	loader *DyndepLoader
}

// NewDyndepParser 创建解析器
func NewDyndepParser(state *graph.State, loader *DyndepLoader) *DyndepParser {
	return &DyndepParser{
		state:  state,
		scope:  make(map[string]string),
		loader: loader,
	}
}

// Parse 解析 dyndep 文件内容
func (p *DyndepParser) Parse(filename, content string) error {
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	haveVersion := false

	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		if idx := strings.Index(raw, "#"); idx >= 0 {
			raw = raw[:idx]
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		// 处理变量赋值（仅支持 ninja_dyndep_version）
		if strings.Contains(line, "=") && !strings.HasPrefix(line, "build") {
			parts := strings.SplitN(line, "=", 2)
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if key == "ninja_dyndep_version" {
				if haveVersion {
					return fmt.Errorf("%s:%d: duplicate version", filename, lineNo)
				}
				version := strings.Trim(val, `"`)
				major, minor := util.ParseVersion(version)
				if major != 1 || minor != 0 {
					return fmt.Errorf("%s:%d: unsupported 'ninja_dyndep_version = %s'", filename, lineNo, version)
				}
				haveVersion = true
			}
			continue
		}

		if !haveVersion {
			return fmt.Errorf("%s:%d: expected 'ninja_dyndep_version = ...'", filename, lineNo)
		}

		// 解析 build 行
		if strings.HasPrefix(line, "build") {
			if err := p.parseBuildLine(filename, lineNo, line); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("%s:%d: unexpected token %q", filename, lineNo, line)
		}
	}
	if !haveVersion {
		return fmt.Errorf("%s: expected 'ninja_dyndep_version = ...'", filename)
	}
	return scanner.Err()
}

// parseBuildLine 解析格式: build output: dyndep | implicit_outputs | implicit_inputs
/*func (p *DyndepParser) parseBuildLine(filename string, lineNo int, line string) error {
	// 去掉 "build"
	rest := strings.TrimSpace(line[len("build"):])
	// 查找冒号
	colonIdx := strings.Index(rest, ":")
	if colonIdx == -1 {
		return fmt.Errorf("%s:%d: missing ':' in build line", filename, lineNo)
	}
	outputsPart := strings.TrimSpace(rest[:colonIdx])
	rest = strings.TrimSpace(rest[colonIdx+1:])

	// 解析输出（仅一个主输出）
	output := strings.Fields(outputsPart)
	if len(output) != 1 {
		return fmt.Errorf("%s:%d: expected exactly one output", filename, lineNo)
	}
	mainOutput := output[0]

	// 查找对应的 Edge
	node := p.state.LookupNode(mainOutput)
	if node == nil || node.Edge == nil {
		return fmt.Errorf("%s:%d: no build statement for '%s'", filename, lineNo, mainOutput)
	}
	edge := node.Edge

	// 检查是否已有 dyndep 记录
	if _, ok := p.loader.dyndepMap[edge]; ok {
		return fmt.Errorf("%s:%d: multiple dyndep statements for '%s'", filename, lineNo, mainOutput)
	}

	// 解析命令名，必须是 "dyndep"
	fields := strings.Fields(rest)
	if len(fields) == 0 || fields[0] != "dyndep" {
		return fmt.Errorf("%s:%d: expected 'dyndep' command", filename, lineNo)
	}
	args := fields[1:]

	// 解析 | 和 ||（只支持 | 用于隐式输入/输出，不支持 ||）
	var implicitOutputs, implicitInputs []string
	state := 0 // 0 = 主输入（无），1 = 隐式输出，2 = 隐式输入
	for _, arg := range args {
		if arg == "|" {
			if state == 0 {
				state = 1
			} else if state == 1 {
				state = 2
			} else {
				return fmt.Errorf("%s:%d: too many '|'", filename, lineNo)
			}
			continue
		} else if arg == "||" {
			return fmt.Errorf("%s:%d: order-only deps not supported in dyndep", filename, lineNo)
		}
		switch state {
		case 1:
			implicitOutputs = append(implicitOutputs, arg)
		case 2:
			implicitInputs = append(implicitInputs, arg)
		default:
			// 忽略（理论上不应出现）
		}
	}

	// 收集隐式输入节点
	var impInputNodes []*graph.Node
	for _, in := range implicitInputs {
		node := p.state.AddNode(util.NormalizePath(in))
		impInputNodes = append(impInputNodes, node)
	}
	// 收集隐式输出节点
	var impOutputNodes []*graph.Node
	for _, out := range implicitOutputs {
		node := p.state.AddNode(util.NormalizePath(out))
		impOutputNodes = append(impOutputNodes, node)
	}

	// 存储到 loader 中
	p.loader.dyndepMap[edge] = &DyndepInfo{
		Restat:          false, // 默认 false，后续可解析 restat 绑定
		ImplicitInputs:  impInputNodes,
		ImplicitOutputs: impOutputNodes,
	}
	return nil
}*/

func (p *DyndepParser) parseBuildLine(filename string, lineNo int, line string) error {
	// 去掉 "build"
	rest := strings.TrimSpace(line[len("build"):])
	// 查找冒号
	colonIdx := strings.Index(rest, ":")
	if colonIdx == -1 {
		return fmt.Errorf("%s:%d: missing ':' in build line", filename, lineNo)
	}
	beforeColon := strings.TrimSpace(rest[:colonIdx])
	afterColon := strings.TrimSpace(rest[colonIdx+1:])

	// 解析冒号前的部分：格式 "output | implicit_outputs"
	var explicitOutput string
	var implicitOutputs []string
	if pipeIdx := strings.Index(beforeColon, "|"); pipeIdx != -1 {
		explicitOutput = strings.TrimSpace(beforeColon[:pipeIdx])
		impOuts := strings.Fields(strings.TrimSpace(beforeColon[pipeIdx+1:]))
		implicitOutputs = append(implicitOutputs, impOuts...)
	} else {
		explicitOutput = beforeColon
	}

	// 解析冒号后的部分：格式 "dyndep | implicit_inputs"
	fields := strings.Fields(afterColon)
	if len(fields) == 0 || fields[0] != "dyndep" {
		return fmt.Errorf("%s:%d: expected 'dyndep' command", filename, lineNo)
	}
	args := fields[1:]

	var implicitInputs []string
	if len(args) > 0 && args[0] == "|" {
		implicitInputs = append(implicitInputs, args[1:]...)
	} else if len(args) > 0 {
		return fmt.Errorf("%s:%d: expected '|' before implicit inputs", filename, lineNo)
	}

	// 查找对应的 Edge
	node := p.state.LookupNode(explicitOutput)
	if node == nil || node.Edge == nil {
		return fmt.Errorf("%s:%d: no build statement for '%s'", filename, lineNo, explicitOutput)
	}
	edge := node.Edge

	// 检查是否已有 dyndep 记录
	if _, ok := p.loader.dyndepMap[edge]; ok {
		return fmt.Errorf("%s:%d: multiple dyndep statements for '%s'", filename, lineNo, explicitOutput)
	}

	// 收集隐式输入节点
	var impInputNodes []*graph.Node
	for _, in := range implicitInputs {
		node := p.state.AddNode(util.NormalizePath(in))
		impInputNodes = append(impInputNodes, node)
	}
	// 收集隐式输出节点
	var impOutputNodes []*graph.Node
	for _, out := range implicitOutputs {
		node := p.state.AddNode(util.NormalizePath(out))
		impOutputNodes = append(impOutputNodes, node)
	}

	// 存储到 loader 中
	p.loader.dyndepMap[edge] = &DyndepInfo{
		Restat:          false, // 后续可解析 restat 绑定
		ImplicitInputs:  impInputNodes,
		ImplicitOutputs: impOutputNodes,
	}
	return nil
}
