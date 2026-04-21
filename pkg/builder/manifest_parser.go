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

func (p *ManifestParser) Load(filename string, err *string, parent *Lexer) bool {
	// 读取文件内容
	var content string
	status := p.fileReader.ReadFile(filename, &content, err)
	if status != util.StatusOkay {
		*err = "loading '" + filename + "': " + *err
		if parent != nil {
			parent.Error(*err, err)
		}
		return false
	}
	return p.Parse(filename, content, err)
}

func (p *ManifestParser) ParseTest(input string, err *string) bool {
	p.quiet = true
	return p.Parse("input", input, err)
}

func (p *ManifestParser) Parse(filename, input string, err *string) bool {
	p.lexer.Start(filename, input)

	for {
		tok := p.lexer.ReadToken()
		switch tok {
		case POOL:
			if !p.parsePool(err) {
				return false
			}
		case BUILD:
			if !p.ParseEdge(err) {
				return false
			}
		case RULE:
			if !p.parseRule(err) {
				return false
			}
		case DEFAULT:
			if !p.ParseDefault(err) {
				return false
			}
		case IDENT:
			p.lexer.UnreadToken()
			var key string
			var val EvalString
			if !p.ParseLet(&key, &val, err) {
				return false
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
			if !p.parseFileInclude(tok == SUBNINJA, err) {
				return false
			}
		case ERROR:
			return p.lexer.Error(p.lexer.DescribeLastError(), err)
		case TEOF:
			return true
		case NEWLINE:
			// ignore
		default:
			return p.lexer.Error("unexpected "+tok.String(), err)
		}
	}
	return false
}

func (p *ManifestParser) parsePool(err *string) bool {
	var name string
	if !p.lexer.ReadIdent(&name) {
		return p.lexer.Error("expected pool name", err)
	}
	if !p.expectToken(NEWLINE, err) {
		return false
	}
	if p.state.LookupPool(name) != nil {
		return p.lexer.Error("duplicate pool '"+name+"'", err)
	}
	depth := -1
	for p.lexer.PeekToken(INDENT) {
		var key string
		var val EvalString
		if !p.ParseLet(&key, &val, err) {
			return false
		}
		if key == "depth" {
			depthStr := val.Evaluate(p.env)
			d, ierr := strconv.Atoi(strings.TrimSpace(depthStr))
			if ierr != nil || d < 0 {
				return p.lexer.Error("invalid pool depth", err)
			}
			depth = d
		} else {
			return p.lexer.Error("expected 'depth', got '"+key+"'", err)
		}
	}
	if depth < 0 {
		return p.lexer.Error("expected 'depth =' line", err)
	}
	p.state.AddPool(&Pool{Name: name, Depth: depth})
	return true
}

func (p *ManifestParser) parseRule(err *string) bool {
	var name string
	succ := p.lexer.ReadIdent(&name)
	if !succ {
		return p.lexer.Error("expected rule name", err)
	}

	if !p.expectToken(NEWLINE, err) {
		return false
	}

	if p.env.LookupRuleCurrentScope(name) != nil {
		return p.lexer.Error("duplicate rule '"+name+"'", err)
	}

	rule := NewRule(name)

	for p.lexer.PeekToken(INDENT) {
		var key string
		var value EvalString
		if !p.ParseLet(&key, &value, err) {
			return false
		}

		if IsReservedBinding(key) {
			rule.AddBinding(key, &value)
		} else {
			return p.lexer.Error("unexpected variable '"+key+"'", err)
		}
	}

	rspfile := rule.GetBinding("rspfile")
	rspfileContent := rule.GetBinding("rspfile_content")
	if (rspfile == nil) != (rspfileContent == nil) {
		return p.lexer.Error("rspfile and rspfile_content need to be both specified", err)
	}

	if rule.GetBinding("command") == nil {
		return p.lexer.Error("expected 'command'", err)
	}

	p.env.AddRule(rule)
	return true
}

func (p *ManifestParser) ParseLet(key *string, val *EvalString, err *string) bool {
	if !p.lexer.ReadIdent(key) {
		return p.lexer.Error("expected variable name", err)
	}
	if !p.expectToken(EQUALS, err) {
		return false
	}

	if !p.lexer.ReadVarValue(val, err) {
		return false
	}
	return true
}

// ParseEdge 解析 build 语句，创建 Edge 并添加到 State。
func (p *ManifestParser) ParseEdge(err *string) bool {
	// 清空临时存储（复用切片）
	p.outs = p.outs[:0]
	p.ins = p.ins[:0]
	p.validations = p.validations[:0]

	// 1. 解析显式输出（可多个，以空格分隔）
	for {
		out := EvalString{}
		if !p.lexer.ReadPath(&out, err) {
			return false
		}
		if out.Empty() {
			break
		}
		// 检查路径求值后是否为单独的 ":"
		path := out.Evaluate(p.env)
		if path == ":" {
			return p.lexer.Error("empty path", err)
		}
		p.outs = append(p.outs, &out)
	}

	// 2. 解析隐式输出（如果有 '|'）
	implicitOuts := 0
	if p.lexer.PeekToken(PIPE) {
		for {
			out := EvalString{}
			if !p.lexer.ReadPath(&out, err) {
				return false
			}
			if out.Empty() {
				break
			}
			// 检查路径求值后是否为单独的 ":"
			path := out.Evaluate(p.env)
			if path == ":" {
				return p.lexer.Error("empty path", err)
			}
			p.outs = append(p.outs, &out)
			implicitOuts++
		}
	}

	if len(p.outs) == 0 {
		return p.lexer.Error("expected path", err)
	}

	// 3. 期望冒号
	if !p.expectToken(COLON, err) {
		return false
	}

	// 4. 规则名
	var ruleName string
	succ := p.lexer.ReadIdent(&ruleName)
	if !succ {
		return p.lexer.Error("expected build command name", err)
	}
	rule := p.env.LookupRule(ruleName)
	if rule == nil {
		return p.lexer.Error("unknown build rule '"+ruleName+"'", err)
	}

	// 5. 解析显式输入
	for {
		in := EvalString{}
		if !p.lexer.ReadPath(&in, err) {
			return false
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
			if !p.lexer.ReadPath(&in, err) {
				return false
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
			if !p.lexer.ReadPath(&in, err) {
				return false
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
			if !p.lexer.ReadPath(&val, err) {
				return false
			}
			if val.Empty() {
				break
			}
			p.validations = append(p.validations, &val)
		}
	}

	// 9. 期望换行
	if !p.expectToken(NEWLINE, err) {
		return false
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
		var key string
		var val EvalString
		if !p.ParseLet(&key, &val, err) {
			return false
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
			return p.lexer.Error("unknown pool name '"+poolName+"'", err)
		}
		edge.Pool = pool
	}

	// 13. 添加输出节点
	for _, outEval := range p.outs {
		path := outEval.Evaluate(env)
		if path == "" || path == ":" {
			return p.lexer.Error("empty path", err)
		}
		var slashBits uint64
		util.CanonicalizePathString(&path, &slashBits)
		if !p.state.AddOut(edge, path, slashBits, err) {
			return p.lexer.Error(*err, err)
		}
	}

	// 如果所有输出都已由其他边生成，则丢弃此边（例如重复定义）
	if len(edge.Outputs) == 0 {
		// 从 state 中移除边（简单实现：从切片末尾删除）
		p.state.Edges = p.state.Edges[:len(p.state.Edges)-1]
		return true
	}
	edge.ImplicitOuts = implicitOuts

	// 14. 添加输入节点
	for _, inEval := range p.ins {
		path := inEval.Evaluate(env)
		if path == "" {
			return p.lexer.Error("empty path", err)
		}
		var slashBits uint64
		util.CanonicalizePathString(&path, &slashBits)
		p.state.AddIn(edge, path, slashBits)
	}
	edge.ImplicitDeps = implicit
	edge.OrderOnlyDeps = orderOnly

	// 15. 添加验证节点
	for _, valEval := range p.validations {
		path := valEval.Evaluate(env)
		if path == "" {
			return p.lexer.Error("empty path", err)
		}
		var slashBits uint64
		util.CanonicalizePathString(&path, &slashBits)
		p.state.AddValidation(edge, path, slashBits)
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
		var slashBits uint64
		util.CanonicalizePathString(&dyndep, &slashBits)
		dyndepNode := p.state.GetNode(dyndep, slashBits)
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
			return p.lexer.Error("dyndep '"+dyndep+"' is not an input", err)
		}
		if edge.DyndepFile.GeneratedByDepLoader == false {
			panic(errors.New("dyndep '" + dyndep + "' is not a dependency"))
		}
	}

	return true
}

// ParseDefault 解析 default 语句，将默认目标添加到 State 中。
func (p *ManifestParser) ParseDefault(err *string) bool {
	// 读取第一个路径
	eval := EvalString{}
	if !p.lexer.ReadPath(&eval, err) {
		return false
	}
	if eval.Empty() {
		return p.lexer.Error("expected target name", err)
	}

	for {
		// 求值路径（在当前环境中展开变量）
		path := eval.Evaluate(p.env)
		if path == "" {
			return p.lexer.Error("empty path", err)
		}
		// 规范化路径（slash_bits 在默认目标中不使用，因为只做查找）
		var slashBits uint64
		util.CanonicalizePathString(&path, &slashBits)
		var default_err string
		if !p.state.AddDefault(path, &default_err) {
			return p.lexer.Error(default_err, err)
		}

		// 尝试读取下一个路径
		eval.Clear()
		next := EvalString{}
		if !p.lexer.ReadPath(&next, err) {
			return false
		}
		if next.Empty() {
			break
		}
		eval = next
	}

	// 期望换行
	if !p.expectToken(NEWLINE, err) {
		return false
	}
	return true
}

func (p *ManifestParser) parseFileInclude(newScope bool, err *string) bool {
	eval := EvalString{}
	if !p.lexer.ReadPath(&eval, err) {
		return false
	}
	path := eval.Evaluate(p.env)
	if path == "" {
		return p.lexer.Error("empty path", err)
	}
	if p.subparser == nil {
		p.subparser = NewManifestParser(p.state, p.fileReader, p.options)
	}
	if newScope {
		p.subparser.env = NewBindingEnv(p.env)
	} else {
		p.subparser.env = p.env
	}
	if !p.subparser.Load(path, err, p.lexer) {
		return false
	}
	if !p.expectToken(NEWLINE, err) {
		return false
	}
	return true
}

func (p *ManifestParser) expectToken(tok Token, err *string) bool {
	if p.lexer.ReadToken() != tok {
		return p.lexer.Error("expected "+tok.String(), err)
	}
	return true
}
