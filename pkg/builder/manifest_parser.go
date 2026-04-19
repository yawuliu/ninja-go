package builder

import (
	"errors"
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

// Verify that *UserCacher implements Cacher
var _ Parser = (*ManifestParser)(nil)

func NewManifestParser(state *State, file_reader util.FileSystem, options ManifestParserOptions) *ManifestParser {
	m := &ManifestParser{}
	m.state = state
	m.fileReader = file_reader
	m.lexer = &Lexer{}
	m.options = options
	m.quiet = false
	m.env = state.Bindings
	return m
}

//func NewManifestParser(state *State, fileReader util.FileSystem, options ManifestParserOptions) *ManifestParser {
//	return &ManifestParser{
//		state:      state,
//		fileReader: fileReader,
//		options:    options,
//		env:        NewBindingEnv(nil),
//	}
//}

func (p *ManifestParser) Load(filename string, parent *BaseParser) error {
	// 读取文件内容
	content, err := p.fileReader.ReadFile(filename)
	if err != nil {
		errMsg := fmt.Sprintf("loading '%s': %v", filename, err)
		if parent != nil {
			parent.lexer.Error(errMsg)
		}
		return fmt.Errorf("%s", errMsg)
	}
	return p.Parse(filename, string(content))
}

func (p *ManifestParser) ParseTest(input string) error {
	p.quiet = true
	return p.Parse("input", input)
}

func (p *ManifestParser) Parse(filename, input string) error {
	p.lexer.Start(filename, input)

	for {
		tok := p.lexer.ReadToken()
		switch tok {
		case POOL:
			if err := p.parsePool(); err != nil {
				return err
			}
		case BUILD:
			if err := p.ParseEdge(); err != nil {
				return err
			}
		case RULE:
			if err := p.parseRule(); err != nil {
				return err
			}
		case DEFAULT:
			if err := p.ParseDefault(); err != nil {
				return err
			}
		case IDENT:
			p.lexer.UnreadToken()
			key, val, err := p.ParseLet()
			if err != nil {
				return err
			}
			value := val.Evaluate(p.env)
			if key == "ninja_required_version" {
				// 解析版本号，设置到 lexer 中
				major, minor := util.ParseVersion(value)
				p.lexer.manifestVersionMajor = major
				p.lexer.manifestVersionMinor = minor
			}
			p.env.AddBinding(key, value)
		case INCLUDE, SUBNINJA:
			if err := p.parseFileInclude(tok == SUBNINJA); err != nil {
				return err
			}
		case ERROR:
			return fmt.Errorf("%s", p.lexer.DescribeLastError())
		case TEOF:
			return nil
		case NEWLINE:
			// ignore
		default:
			return fmt.Errorf("unexpected token %s", tok.String())
		}
	}
	return errors.New(" not reached")
}

func (p *ManifestParser) parsePool() error {
	var name string
	succ := p.lexer.ReadIdent(&name)
	if !succ {
		return errors.New("expected pool name")
	}
	if err := p.expectToken(NEWLINE); err != nil {
		return err
	}
	if p.state.LookupPool(name) != nil {
		return p.lexer.Error("duplicate pool '" + name + "'")
	}
	depth := -1
	for p.lexer.PeekToken(INDENT) {
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
			return p.lexer.Error("expected 'depth', got '" + key + "'")
		}
	}
	if depth < 0 {
		return p.lexer.Error("expected 'depth =' line")
	}
	p.state.AddPool(&Pool{Name: name, Depth: depth})
	return nil
}

func (p *ManifestParser) parseRule() error {
	var name string
	succ := p.lexer.ReadIdent(&name)
	if !succ {
		return p.lexer.Error("expected rule name")
	}

	if err := p.expectToken(NEWLINE); err != nil {
		return err
	}

	if p.env.LookupRuleCurrentScope(name) != nil {
		return p.lexer.Error("duplicate rule '" + name + "'")
	}

	rule := NewRule(name)

	for p.lexer.PeekToken(INDENT) {
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
		return p.lexer.Error("expected 'command'")
	}

	p.env.AddRule(rule)
	return nil
}

func (p *ManifestParser) ParseLet() (string, *EvalString, error) {
	var key string
	succ := p.lexer.ReadIdent(&key)
	if !succ {
		return "", nil, errors.New("expected variable name")
	}
	if err := p.expectToken(EQUALS); err != nil {
		return "", nil, err
	}
	val := EvalString{}
	_, err := p.lexer.ReadVarValue(&val)
	if err != nil {
		return "", nil, err
	}
	return key, &val, nil
}

// ParseEdge 解析 build 语句，创建 Edge 并添加到 State。
func (p *ManifestParser) ParseEdge() error {
	// 清空临时存储（复用切片）
	p.outs = p.outs[:0]
	p.ins = p.ins[:0]
	p.validations = p.validations[:0]

	// 1. 解析显式输出（可多个，以空格分隔）
	for {
		out := EvalString{}
		_, err := p.lexer.ReadPath(&out)
		if err != nil {
			return err
		}
		if out.Empty() {
			break
		}
		// 检查路径求值后是否为单独的 ":"
		path := out.Evaluate(p.env)
		if path == ":" {
			return p.lexer.Error("empty path")
		}
		p.outs = append(p.outs, &out)
	}

	// 2. 解析隐式输出（如果有 '|'）
	implicitOuts := 0
	if p.lexer.PeekToken(PIPE) {
		for {
			out := EvalString{}
			_, err := p.lexer.ReadPath(&out)
			if err != nil {
				return err
			}
			if out.Empty() {
				break
			}
			// 检查路径求值后是否为单独的 ":"
			path := out.Evaluate(p.env)
			if path == ":" {
				return p.lexer.Error("empty path")
			}
			p.outs = append(p.outs, &out)
			implicitOuts++
		}
	}

	if len(p.outs) == 0 {
		return p.lexer.Error("expected path")
	}

	// 3. 期望冒号
	if err := p.expectToken(COLON); err != nil {
		return err
	}

	// 4. 规则名
	var ruleName string
	succ := p.lexer.ReadIdent(&ruleName)
	if !succ {
		return p.lexer.Error("expected build command name")
	}
	rule := p.env.LookupRule(ruleName)
	if rule == nil {
		return p.lexer.Error("unknown build rule '" + ruleName + "'")
	}

	// 5. 解析显式输入
	for {
		in := EvalString{}
		_, err := p.lexer.ReadPath(&in)
		if err != nil {
			return err
		}
		if in.Empty() {
			break
		}
		p.ins = append(p.ins, &in)
	}

	// 6. 隐式输入（'|' 后）
	implicit := 0
	if p.lexer.PeekToken(PIPE) {
		for {
			in := EvalString{}
			_, err := p.lexer.ReadPath(&in)
			if err != nil {
				return err
			}
			if in.Empty() {
				break
			}
			p.ins = append(p.ins, &in)
			implicit++
		}
	}

	// 7. order-only 输入（'||' 后）
	orderOnly := 0
	if p.lexer.PeekToken(PIPE2) {
		for {
			in := EvalString{}
			_, err := p.lexer.ReadPath(&in)
			if err != nil {
				return err
			}
			if in.Empty() {
				break
			}
			p.ins = append(p.ins, &in)
			orderOnly++
		}
	}

	// 8. 验证边（'|@' 后）
	if p.lexer.PeekToken(PIPEAT) {
		for {
			val := EvalString{}
			_, err := p.lexer.ReadPath(&val)
			if err != nil {
				return err
			}
			if val.Empty() {
				break
			}
			p.validations = append(p.validations, &val)
		}
	}

	// 9. 期望换行
	if err := p.expectToken(NEWLINE); err != nil {
		return err
	}

	// 10. 缩进内的变量绑定（为边创建独立作用域）
	hasIndent := p.lexer.PeekToken(INDENT)
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
		hasIndent = p.lexer.PeekToken(INDENT)
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
		if path == "" || path == ":" {
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
		if edge.DyndepFile.GeneratedByDepLoader == false {
			panic(errors.New("dyndep '" + dyndep + "' is not a dependency"))
		}
	}

	return nil
}

// ParseDefault 解析 default 语句，将默认目标添加到 State 中。
func (p *ManifestParser) ParseDefault() error {
	// 读取第一个路径
	eval := EvalString{}
	_, err := p.lexer.ReadPath(&eval)
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
		next := EvalString{}
		_, err := p.lexer.ReadPath(&next)
		if err != nil {
			return err
		}
		if next.Empty() {
			break
		}
		eval = next
	}

	// 期望换行
	if err := p.expectToken(NEWLINE); err != nil {
		return err
	}
	return nil
}

func (p *ManifestParser) parseFileInclude(newScope bool) error {
	eval := EvalString{}
	_, err := p.lexer.ReadPath(&eval)
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
	return p.expectToken(NEWLINE)
}

func (p *ManifestParser) expectToken(tok Token) error {
	if p.lexer.ReadToken() != tok {
		return p.lexer.Error("expected " + tok.String())
	}
	return nil
}
