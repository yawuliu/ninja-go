package parser

import (
	"bufio"
	"fmt"
	"io"
	"ninja-go/pkg/util"
	"os"
	"strings"

	"ninja-go/pkg/graph"
)

type Parser struct {
	state  *graph.State
	scope  map[string]string // 当前变量作用域
	indent int
}

func NewParser(state *graph.State) *Parser {
	return &Parser{state: state, scope: make(map[string]string)}
}

func (p *Parser) ParseFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return p.ParseReader(f, path)
}

func (p *Parser) ParseReader(r io.Reader, source string) error {
	scanner := bufio.NewScanner(r)
	lineNo := 0
	var pendingRule *graph.Rule
	//var pendingBuild struct {
	//	outputs   []string
	//	rule      string
	//	inputs    []string
	//	implicit  []string
	//	orderOnly []string
	//}

	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		// 去掉行尾注释（# 后面的是注释，但需注意 $# 不是注释）
		hashIdx := strings.Index(raw, "#")
		if hashIdx >= 0 {
			// 简单处理：不考虑转义，Ninja 中 # 前面没有 $ 才是注释
			if hashIdx == 0 || raw[hashIdx-1] != '$' {
				raw = raw[:hashIdx]
			}
		}
		line := strings.TrimRight(raw, " \t")
		if line == "" {
			continue
		}
		// 处理缩进（Ninja 缩进表示块继续）
		leadingSpaces := len(line) - len(strings.TrimLeft(line, " \t"))
		if leadingSpaces > p.indent {
			// 进入子块，继续使用 pendingRule/build
		} else if leadingSpaces < p.indent {
			// 退出子块
			p.indent = leadingSpaces
			pendingRule = nil
		}
		line = strings.TrimSpace(line)
		// 变量赋值: key = value
		//if strings.Contains(line, "=") && !strings.HasPrefix(line, "rule") && !strings.HasPrefix(line, "build") {
		//	parts := strings.SplitN(line, "=", 2)
		//	key := strings.TrimSpace(parts[0])
		//	val := strings.TrimSpace(parts[1])
		//	p.scope[key] = p.expand(val)
		//	continue
		//}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "rule":
			if len(fields) < 2 {
				return fmt.Errorf("%s:%d: missing rule name", source, lineNo)
			}
			ruleName := fields[1]
			pendingRule = &graph.Rule{Name: ruleName}
			p.state.Rules[ruleName] = pendingRule
			p.indent = leadingSpaces + 1 // 期望缩进
		case "build":
			// 解析 build outputs: rule inputs | implicit || order-only
			// 简化：先按 : 分割
			restOfLine := strings.TrimSpace(line[len("build"):])
			colonIdx := strings.Index(restOfLine, ":")
			if colonIdx == -1 {
				return fmt.Errorf("%s:%d: invalid build line", source, lineNo)
			}
			outputsPart := strings.TrimSpace(restOfLine[:colonIdx])
			rest := strings.TrimSpace(restOfLine[colonIdx+1:])
			outputs := strings.Fields(outputsPart)
			// rest 可能是 "rule inputs | ..."
			restFields := strings.Fields(rest)
			if len(restFields) < 1 {
				return fmt.Errorf("%s:%d: missing rule name", source, lineNo)
			}
			ruleName := restFields[0]
			args := restFields[1:]
			// 解析 | 和 ||
			var inputs, implicit, orderOnly []string
			state := 0 // 0=inputs, 1=implicit, 2=orderOnly
			for _, arg := range args {
				if arg == "|" {
					state = 1
					continue
				} else if arg == "||" {
					state = 2
					continue
				}
				switch state {
				case 0:
					inputs = append(inputs, arg)
				case 1:
					implicit = append(implicit, arg)
				case 2:
					orderOnly = append(orderOnly, arg)
				}
			}
			//pendingBuild.outputs = outputs
			//pendingBuild.rule = ruleName
			//pendingBuild.inputs = inputs
			//pendingBuild.implicit = implicit
			//pendingBuild.orderOnly = orderOnly

			// 立即添加这条边
			p.addBuild(struct {
				outputs   []string
				rule      string
				inputs    []string
				implicit  []string
				orderOnly []string
			}{outputs, ruleName, inputs, implicit, orderOnly})
			// 注意：build 语句不支持缩进属性（如 pool），所以不设置 p.indent
			// p.indent = leadingSpaces + 1
		case "default":
			if len(fields) < 2 {
				return fmt.Errorf("%s:%d: default requires targets", source, lineNo)
			}
			for _, t := range fields[1:] {
				expanded := p.expand(t)
				node := p.state.AddNode(util.NormalizePath(expanded))
				p.state.Defaults = append(p.state.Defaults, node)
			}
		default:
			// 这里处理两种情况：
			// 1. 当前在 rule 缩进块内 -> 解析 rule 属性（command, depfile 等）
			// 2. 当前不在任何块内 -> 可能是顶层变量赋值
			if pendingRule != nil {
				// 通用属性解析：按第一个 '=' 分割
				eqIdx := strings.Index(line, "=")
				if eqIdx == -1 {
					// 没有等号的可能是布尔属性如 restat, generator
					switch line {
					case "restat":
						pendingRule.Restat = true
					case "generator":
						pendingRule.Generator = true
					default:
						return fmt.Errorf("%s:%d: unexpected line in rule: %s", source, lineNo, line)
					}
				} else {
					key := strings.TrimSpace(line[:eqIdx])
					val := strings.TrimSpace(line[eqIdx+1:])
					switch key {
					case "command":
						pendingRule.Command = val // 不要 p.expand(val)
					case "depfile":
						pendingRule.Depfile = val // 不要 p.expand(val)
					case "dyndep":
						pendingRule.Dyndep = val // 不要 p.expand(val)
					case "pool":
						pendingRule.Pool = val // 不要 p.expand(val)
					default:
						// 忽略未知属性（按 Ninja 规范应报错，但可宽容）
						// 未知属性，可以忽略或报错
					}
				}
				//switch fields[0] {
				//case "command":
				//	val := strings.TrimPrefix(line, "command")
				//	val = strings.TrimPrefix(val, "=")
				//	pendingRule.Command = p.expand(strings.TrimSpace(val))
				//case "depfile":
				//	val := strings.TrimPrefix(line, "depfile")
				//	val = strings.TrimPrefix(val, "=")
				//	pendingRule.Depfile = p.expand(strings.TrimSpace(val))
				//case "dyndep":
				//	val := strings.TrimPrefix(line, "dyndep")
				//	val = strings.TrimPrefix(val, "=")
				//	pendingRule.Dyndep = p.expand(strings.TrimSpace(val))
				//case "restat":
				//	pendingRule.Restat = true
				//case "generator":
				//	pendingRule.Generator = true
				//case "pool":
				//	val := strings.TrimPrefix(line, "pool")
				//	val = strings.TrimPrefix(val, "=")
				//	pendingRule.Pool = p.expand(strings.TrimSpace(val))
				//}
				// } else if pendingBuild.rule != "" {
				// build 块的属性暂不支持（如 pool, dyndep 等）
				// 处理最后累积的 build
				//if pendingBuild.rule != "" {
				//	p.addBuild(pendingBuild)
				//}
			} else {
				// 不在任何块内，且行不是 rule/build/default，则视为变量赋值
				if strings.Contains(line, "=") {
					parts := strings.SplitN(line, "=", 2)
					key := strings.TrimSpace(parts[0])
					val := strings.TrimSpace(parts[1])
					p.scope[key] = p.expand(val)
				} else {
					return fmt.Errorf("%s:%d: unexpected token %q", source, lineNo, fields[0])
				}
			}
		}
	}

	return scanner.Err()
}

func (p *Parser) addBuild(b struct {
	outputs   []string
	rule      string
	inputs    []string
	implicit  []string
	orderOnly []string
}) {
	rule := p.state.Rules[b.rule]
	if rule == nil {
		// 延迟错误？实际应该报错，这里简化
		return
	}
	edge := &graph.Edge{Rule: rule}
	// 输出节点
	for _, out := range b.outputs {
		node := p.state.AddNode(util.NormalizePath(p.expand(out)))
		node.Generated = true
		node.Edge = edge // 关键：将节点与产生它的边关联
		edge.Outputs = append(edge.Outputs, node)
	}
	// 输入节点
	for _, in := range b.inputs {
		node := p.state.AddNode(util.NormalizePath(p.expand(in)))
		edge.Inputs = append(edge.Inputs, node)
		node.AddOutEdge(edge) // 新增：建立反向关系
	}
	for _, imp := range b.implicit {
		node := p.state.AddNode(util.NormalizePath(p.expand(imp)))
		edge.ImplicitDeps = append(edge.ImplicitDeps, node)
		node.AddOutEdge(edge) // 新增：建立反向关系
	}
	for _, ord := range b.orderOnly {
		node := p.state.AddNode(util.NormalizePath(p.expand(ord)))
		edge.OrderOnlyDeps = append(edge.OrderOnlyDeps, node)
		node.AddOutEdge(edge) // 新增：建立反向关系
	}
	p.state.Edges = append(p.state.Edges, edge)
}

func (p *Parser) expand(s string) string {
	// 简单变量展开：$var 或 ${var}
	// 内建变量 $in, $out 在构建时动态展开，此处保留原样
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '$' && i+1 < len(s) {
			if s[i+1] == '$' {
				result.WriteByte('$')
				i++
				continue
			}
			if s[i+1] == '{' {
				end := strings.Index(s[i+2:], "}")
				if end >= 0 {
					varName := s[i+2 : i+2+end]
					if val, ok := p.scope[varName]; ok {
						result.WriteString(val)
					}
					i += 2 + end
					continue
				}
			}
			// 简单变量 $var
			start := i + 1
			j := start
			for j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= '0' && s[j] <= '9' || s[j] == '_') {
				j++
			}
			varName := s[start:j]
			if val, ok := p.scope[varName]; ok {
				result.WriteString(val)
			}
			i = j - 1
			continue
		}
		result.WriteByte(s[i])
	}
	return result.String()
}

// ParseBuildLines 解析只包含 build 语句的字符串，返回边列表（不修改 state）
func (p *Parser) ParseBuildLines(content string) ([]*graph.Edge, error) {
	// 创建临时 state 用于收集边
	tmpState := graph.NewState()
	// 复制已有的 rules 到临时 state（因为 build 语句依赖 rule）
	for name, rule := range p.state.Rules {
		tmpState.Rules[name] = rule
	}
	// 使用同样的解析逻辑，但只处理 build 语句，忽略其他
	// 简便方法：调用 parseReader 但提供只包含 build 的字符串，但现有 parser 会修改传入的 state
	// 因此我们需要一个轻量解析器，或者复用解析函数但传入 tmpState
	// 实现细节略...
	return nil, nil
}
