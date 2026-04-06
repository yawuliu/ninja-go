package builder

import (
	"fmt"
	"ninja-go/pkg/util"
	"os"
	"strconv"
	"strings"
)

type PhonyCycleAction int

const (
	PhonyCycleActionWarn PhonyCycleAction = iota
	PhonyCycleActionError
)

type ManifestParserOptions struct {
	PhonyCycleAction PhonyCycleAction
}

type ManifestParser struct {
	state       *State
	fileReader  util.FileSystem
	options     ManifestParserOptions
	quiet       bool
	env         *BindingEnv
	subparser   *ManifestParser
	ins         []*EvalString
	outs        []*EvalString
	validations []*EvalString
	lexer       *Lexer
}

func NewManifestParser(state *State, fileReader util.FileSystem, options ManifestParserOptions) *ManifestParser {
	return &ManifestParser{
		state:      state,
		fileReader: fileReader,
		options:    options,
		env:        NewBindingEnv(nil),
	}
}

func (p *ManifestParser) ParseTest(input string) error {
	p.quiet = true
	return p.Parse("input", input)
}

func (p *ManifestParser) Parse(filename, input string) error {
	p.lexer = NewLexer(input)
	p.lexer.Start(filename, input)

	for {
		tok := p.lexer.ReadToken()
		switch tok {
		case T_POOL:
			if err := p.parsePool(); err != nil {
				return err
			}
		case T_BUILD:
			if err := p.ParseEdge(); err != nil {
				return err
			}
		case T_RULE:
			if err := p.parseRule(); err != nil {
				return err
			}
		case T_DEFAULT:
			if err := p.ParseDefault(); err != nil {
				return err
			}
		case T_IDENT:
			p.lexer.UnreadToken()
			key, val, err := p.ParseLet()
			if err != nil {
				return err
			}
			value := val.Evaluate(p.env)
			if key == "ninja_required_version" {
				// 解析版本号，设置到 lexer 中
				major, minor := util.ParseVersion(value)
				p.lexer.ManifestVersionMajor = major
				p.lexer.ManifestVersionMinor = minor
			}
			p.env.AddBinding(key, value)
		case T_INCLUDE, T_SUBNINJA:
			if err := p.parseFileInclude(tok == T_SUBNINJA); err != nil {
				return err
			}
		case T_ERROR:
			return fmt.Errorf("%s", p.lexer.DescribeLastError())
		case T_EOF:
			return nil
		case T_NEWLINE:
			// ignore
		default:
			return fmt.Errorf("unexpected token %s", TokenName(tok))
		}
	}
}

func (p *ManifestParser) parsePool() error {
	name, err := p.lexer.ReadIdent()
	if err != nil {
		return err
	}
	if err := p.expectToken(T_NEWLINE); err != nil {
		return err
	}
	if p.state.LookupPool(name) != nil {
		return p.lexer.Error("duplicate pool '" + name + "'")
	}
	depth := -1
	for p.lexer.PeekToken(T_INDENT) {
		key, val, err := p.ParseLet()
		if err != nil {
			return err
		}
		if key == "depth" {
			depthStr := val.Evaluate(p.env)
			d, err := strconv.Atoi(strings.TrimSpace(depthStr))
			if err != nil || d < 0 {
				return p.lexer.Error("invalid pool depth")
			}
			depth = d
		} else {
			return p.lexer.Error("unexpected variable '" + key + "'")
		}
	}
	if depth < 0 {
		return p.lexer.Error("expected 'depth =' line")
	}
	p.state.AddPool(&Pool{Name: name, Depth: depth})
	return nil
}

func (p *ManifestParser) parseRule() error {
	name, err := p.lexer.ReadIdent()
	if err != nil {
		return p.lexer.Error("expected rule name")
	}

	if err := p.expectToken(T_NEWLINE); err != nil {
		return err
	}

	if p.env.LookupRuleCurrentScope(name) != nil {
		return p.lexer.Error("duplicate rule '" + name + "'")
	}

	rule := NewRule(name)

	for p.lexer.PeekToken(T_INDENT) {
		key, value, err := p.ParseLet()
		if err != nil {
			return err
		}

		if IsReservedBinding(key) {
			rule.AddBinding(key, value)
		} else {
			return p.lexer.Error("unexpected variable '" + key + "'")
		}
	}

	rspfile := rule.GetBinding("rspfile")
	rspfileContent := rule.GetBinding("rspfile_content")
	if (rspfile == nil) != (rspfileContent == nil) {
		return p.lexer.Error("rspfile and rspfile_content need to be both specified")
	}

	if rule.GetBinding("command") == nil {
		return p.lexer.Error("expected 'command =' line")
	}

	p.env.AddRule(rule)
	return nil
}

func (p *ManifestParser) ParseLet() (string, *EvalString, error) {
	key, err := p.lexer.ReadIdent()
	if err != nil {
		return "", nil, err
	}
	if err := p.expectToken(T_EQUALS); err != nil {
		return "", nil, err
	}
	val, err := p.lexer.ReadVarValue()
	if err != nil {
		return "", nil, err
	}
	return key, val, nil
}

// ParseEdge 解析 build 语句，创建 Edge 并添加到 State。
func (p *ManifestParser) ParseEdge() error {
	// 清空临时存储（复用切片）
	p.outs = p.outs[:0]
	p.ins = p.ins[:0]
	p.validations = p.validations[:0]

	// 1. 解析显式输出（可多个，以空格分隔）
	for {
		out, err := p.lexer.ReadPath()
		if err != nil {
			return err
		}
		if out.Empty() {
			break
		}
		p.outs = append(p.outs, out)
	}

	// 2. 解析隐式输出（如果有 '|'）
	implicitOuts := 0
	if p.lexer.PeekToken(T_PIPE) {
		for {
			out, err := p.lexer.ReadPath()
			if err != nil {
				return err
			}
			if out.Empty() {
				break
			}
			p.outs = append(p.outs, out)
			implicitOuts++
		}
	}

	if len(p.outs) == 0 {
		return p.lexer.Error("expected path")
	}

	// 3. 期望冒号
	if err := p.expectToken(T_COLON); err != nil {
		return err
	}

	// 4. 规则名
	ruleName, err := p.lexer.ReadIdent()
	if err != nil {
		return p.lexer.Error("expected build command name")
	}
	rule := p.env.LookupRule(ruleName)
	if rule == nil {
		return p.lexer.Error("unknown build rule '" + ruleName + "'")
	}

	// 5. 解析显式输入
	for {
		in, err := p.lexer.ReadPath()
		if err != nil {
			return err
		}
		if in.Empty() {
			break
		}
		p.ins = append(p.ins, in)
	}

	// 6. 隐式输入（'|' 后）
	implicit := 0
	if p.lexer.PeekToken(T_PIPE) {
		for {
			in, err := p.lexer.ReadPath()
			if err != nil {
				return err
			}
			if in.Empty() {
				break
			}
			p.ins = append(p.ins, in)
			implicit++
		}
	}

	// 7. order-only 输入（'||' 后）
	orderOnly := 0
	if p.lexer.PeekToken(T_PIPE2) {
		for {
			in, err := p.lexer.ReadPath()
			if err != nil {
				return err
			}
			if in.Empty() {
				break
			}
			p.ins = append(p.ins, in)
			orderOnly++
		}
	}

	// 8. 验证边（'|@' 后）
	if p.lexer.PeekToken(T_PIPEAT) {
		for {
			val, err := p.lexer.ReadPath()
			if err != nil {
				return err
			}
			if val.Empty() {
				break
			}
			p.validations = append(p.validations, val)
		}
	}

	// 9. 期望换行
	if err := p.expectToken(T_NEWLINE); err != nil {
		return err
	}

	// 10. 缩进内的变量绑定（为边创建独立作用域）
	hasIndent := p.lexer.PeekToken(T_INDENT)
	var env *BindingEnv
	if hasIndent {
		env = NewBindingEnv(p.env)
	} else {
		env = p.env
	}
	for hasIndent {
		key, val, err := p.ParseLet()
		if err != nil {
			return err
		}
		// 求值并添加到边的环境（值在全局环境求值，然后存入边的环境）
		evaluated := val.Evaluate(p.env)
		env.AddBinding(key, evaluated)
		hasIndent = p.lexer.PeekToken(T_INDENT)
	}

	// 11. 创建 Edge
	edge := p.state.AddEdge(rule)
	edge.Env = env

	// 12. 处理 pool
	poolName := edge.GetBinding("pool")
	if poolName != "" {
		pool := p.state.LookupPool(poolName)
		if pool == nil {
			return p.lexer.Error("unknown pool name '" + poolName + "'")
		}
		edge.Pool = pool
	}

	// 13. 添加输出节点
	for _, outEval := range p.outs {
		path := outEval.Evaluate(env)
		if path == "" {
			return p.lexer.Error("empty path")
		}
		norm, slashBits := util.CanonicalizePath(path)
		if err := p.state.AddOut(edge, norm, slashBits); err != nil {
			return p.lexer.Error(err.Error())
		}
	}

	// 如果所有输出都已由其他边生成，则丢弃此边（例如重复定义）
	if len(edge.Outputs) == 0 {
		// 从 state 中移除边（简单实现：从切片末尾删除）
		p.state.Edges = p.state.Edges[:len(p.state.Edges)-1]
		return nil
	}
	edge.ImplicitOuts = implicitOuts

	// 14. 添加输入节点
	for _, inEval := range p.ins {
		path := inEval.Evaluate(env)
		if path == "" {
			return p.lexer.Error("empty path")
		}
		norm, slashBits := util.CanonicalizePath(path)
		p.state.AddIn(edge, norm, slashBits)
	}
	edge.ImplicitDeps = implicit
	edge.OrderOnlyDeps = orderOnly

	// 15. 添加验证节点
	for _, valEval := range p.validations {
		path := valEval.Evaluate(env)
		if path == "" {
			return p.lexer.Error("empty path")
		}
		norm, slashBits := util.CanonicalizePath(path)
		p.state.AddValidation(edge, norm, slashBits)
	}

	// 16. 处理 phony 自引用（兼容旧 CMake）
	if p.options.PhonyCycleAction == PhonyCycleActionWarn && edge.MaybePhonyCycleDiagnostic() {
		outNode := edge.Outputs[0]
		// 从输入中移除自引用的节点
		newInputs := make([]*Node, 0, len(edge.Inputs))
		for _, in := range edge.Inputs {
			if in != outNode {
				newInputs = append(newInputs, in)
			}
		}
		if len(newInputs) != len(edge.Inputs) {
			edge.Inputs = newInputs
			if !p.quiet {
				fmt.Fprintf(os.Stderr, "ninja: warning: phony target '%s' names itself as an input; ignoring [-w phonycycle=warn]\n", outNode.Path)
			}
		}
	}

	// 17. 处理 dyndep 绑定
	dyndep := edge.GetUnescapedDyndep()
	if dyndep != "" {
		norm, slashBits := util.CanonicalizePath(dyndep)
		dyndepNode := p.state.GetNode(norm, slashBits)
		edge.DyndepFile = dyndepNode
		dyndepNode.DyndepPending = true
		// 验证 dyndep 节点必须是边的输入之一
		found := false
		for _, in := range edge.Inputs {
			if in == dyndepNode {
				found = true
				break
			}
		}
		if !found {
			return p.lexer.Error("dyndep '" + dyndep + "' is not an input")
		}
	}

	return nil
}

// ParseDefault 解析 default 语句，将默认目标添加到 State 中。
func (p *ManifestParser) ParseDefault() error {
	// 读取第一个路径
	eval, err := p.lexer.ReadPath()
	if err != nil {
		return err
	}
	if eval.Empty() {
		return p.lexer.Error("expected target name")
	}

	for {
		// 求值路径（在当前环境中展开变量）
		path := eval.Evaluate(p.env)
		if path == "" {
			return p.lexer.Error("empty path")
		}
		// 规范化路径（slash_bits 在默认目标中不使用，因为只做查找）
		norm, _ := util.CanonicalizePath(path)
		if err := p.state.AddDefault(norm); err != nil {
			return p.lexer.Error(err.Error())
		}

		// 尝试读取下一个路径
		eval.Clear()
		next, err := p.lexer.ReadPath()
		if err != nil {
			return err
		}
		if next.Empty() {
			break
		}
		eval = next
	}

	// 期望换行
	if err := p.expectToken(T_NEWLINE); err != nil {
		return err
	}
	return nil
}

func (p *ManifestParser) parseFileInclude(newScope bool) error {
	eval, err := p.lexer.ReadPath()
	if err != nil {
		return err
	}
	path := eval.Evaluate(p.env)
	if path == "" {
		return p.lexer.Error("empty path")
	}
	if p.subparser == nil {
		p.subparser = NewManifestParser(p.state, p.fileReader, p.options)
	}
	if newScope {
		p.subparser.env = NewBindingEnv(p.env)
	} else {
		p.subparser.env = p.env
	}
	// 读取文件内容
	content, err := p.fileReader.ReadFile(path)
	if err != nil {
		return fmt.Errorf("loading '%s': %v", path, err)
	}
	if err := p.subparser.Parse(path, string(content)); err != nil {
		return err
	}
	return p.expectToken(T_NEWLINE)
}

func (p *ManifestParser) expectToken(tok Token) error {
	if p.lexer.ReadToken() != tok {
		return p.lexer.Error("expected " + TokenName(tok))
	}
	return nil
}
