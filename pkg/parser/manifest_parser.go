package parser

import (
	"fmt"
	"ninja-go/pkg/graph"
	"ninja-go/pkg/util"
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
	state       *graph.State
	fileReader  util.FileSystem
	options     ManifestParserOptions
	quiet       bool
	env         *graph.BindingEnv
	subparser   *ManifestParser
	ins         []*graph.EvalString
	outs        []*graph.EvalString
	validations []*graph.EvalString
	lexer       *Lexer
}

func NewManifestParser(state *graph.State, fileReader util.FileSystem, options ManifestParserOptions) *ManifestParser {
	return &ManifestParser{
		state:      state,
		fileReader: fileReader,
		options:    options,
		env:        graph.NewBindingEnv(nil),
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
			if err := p.parseEdge(); err != nil {
				return err
			}
		case T_RULE:
			if err := p.parseRule(); err != nil {
				return err
			}
		case T_DEFAULT:
			if err := p.parseDefault(); err != nil {
				return err
			}
		case T_IDENT:
			p.lexer.UnreadToken()
			key, val, err := p.parseLet()
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
		key, val, err := p.parseLet()
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
	p.state.AddPool(&graph.Pool{Name: name, Depth: depth})
	return nil
}

func (p *ManifestParser) parseRule() error {
	name, err := p.lexer.ReadIdent()
	if err != nil {
		return err
	}
	if err := p.expectToken(T_NEWLINE); err != nil {
		return err
	}
	if p.env.LookupRule(name) != nil {
		return p.lexer.Error("duplicate rule '" + name + "'")
	}
	rule := &graph.Rule{Name: name}
	for p.lexer.PeekToken(T_INDENT) {
		key, val, err := p.parseLet()
		if err != nil {
			return err
		}
		switch key {
		case "command", "depfile", "dyndep", "pool", "rspfile", "rspfile_content":
			// 存储到 rule 的绑定（需要 Rule 有 Bindings map）
			// 简化：直接设置字段
			if key == "command" {
				rule.Command = val.Evaluate(p.env)
			} else if key == "depfile" {
				rule.Depfile = val.Evaluate(p.env)
			} else if key == "dyndep" {
				rule.Dyndep = val.Evaluate(p.env)
			} else if key == "pool" {
				rule.Pool = val.Evaluate(p.env)
			}
			// rspfile 等暂忽略
		case "restat":
			rule.Restat = true
		case "generator":
			rule.Generator = true
		default:
			return p.lexer.Error("unexpected variable '" + key + "'")
		}
	}
	if rule.Command == "" {
		return p.lexer.Error("expected 'command =' line")
	}
	p.env.AddRule(rule)
	return nil
}

func (p *ManifestParser) parseLet() (string, *graph.EvalString, error) {
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

func (p *ManifestParser) parseEdge() error {
	p.outs = nil
	p.ins = nil
	p.validations = nil

	// 读取显式输出
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
	// 隐式输出（如果有 |）
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
	if err := p.expectToken(T_COLON); err != nil {
		return err
	}
	ruleName, err := p.lexer.ReadIdent()
	if err != nil {
		return err
	}
	rule := p.env.LookupRule(ruleName)
	if rule == nil {
		return p.lexer.Error("unknown build rule '" + ruleName + "'")
	}
	// 显式输入
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
	// 隐式输入（|）
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
	// order-only 输入（||）
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
	// 验证边（|@）
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
	if err := p.expectToken(T_NEWLINE); err != nil {
		return err
	}
	// 处理缩进块（绑定）
	env := p.env
	if p.lexer.PeekToken(T_INDENT) {
		env = graph.NewBindingEnv(p.env)
		for p.lexer.PeekToken(T_INDENT) {
			key, val, err := p.parseLet()
			if err != nil {
				return err
			}
			env.AddBinding(key, val.Evaluate(p.env))
		}
	}
	// 创建 Edge
	edge := p.state.AddEdge(rule)
	edge.Env = env
	// 处理 pool
	if poolName := edge.GetBinding("pool"); poolName != "" {
		pool := p.state.LookupPool(poolName)
		if pool == nil {
			return p.lexer.Error("unknown pool name '" + poolName + "'")
		}
		edge.Pool = pool
	}
	// 添加输出节点
	for i, outEval := range p.outs {
		path := outEval.Evaluate(env)
		if path == "" {
			return p.lexer.Error("empty path")
		}
		node := p.state.AddNode(path)
		node.Generated = true
		node.Edge = edge
		edge.Outputs = append(edge.Outputs, node)
		if i >= len(p.outs)-implicitOuts {
			// 隐式输出
			edge.ImplicitOuts++
		}
	}
	// 添加输入节点
	totalInputs := len(p.ins)
	for i, inEval := range p.ins {
		path := inEval.Evaluate(env)
		if path == "" {
			return p.lexer.Error("empty path")
		}
		node := p.state.AddNode(path)
		edge.Inputs = append(edge.Inputs, node)
		if i >= totalInputs-orderOnly {
			edge.OrderOnlyDeps++
		} else if i >= totalInputs-orderOnly-implicit {
			edge.ImplicitDeps++
		}
	}
	// 添加验证节点
	for _, valEval := range p.validations {
		path := valEval.Evaluate(env)
		if path == "" {
			return p.lexer.Error("empty path")
		}
		node := p.state.AddNode(path)
		edge.Validations = append(edge.Validations, node)
	}
	// 处理 dyndep
	dyndep := edge.GetBinding("dyndep")
	if dyndep != "" {
		dyndepNode := p.state.LookupNode(dyndep)
		if dyndepNode == nil {
			return p.lexer.Error("dyndep '" + dyndep + "' is not an input")
		}
		edge.DyndepFile = dyndepNode
		dyndepNode.DyndepPending = true
		// 确保 dyndep 在输入中
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
	// 处理 phony 自引用（兼容旧 CMake）
	if p.options.PhonyCycleAction == PhonyCycleActionWarn && rule.Name == "phony" && len(edge.Outputs) == 1 {
		out := edge.Outputs[0]
		newInputs := make([]*graph.Node, 0, len(edge.Inputs))
		for _, in := range edge.Inputs {
			if in != out {
				newInputs = append(newInputs, in)
			} else if !p.quiet {
				fmt.Printf("warning: phony target '%s' names itself as an input; ignoring\n", out.Path)
			}
		}
		edge.Inputs = newInputs
	}
	return nil
}

func (p *ManifestParser) parseDefault() error {
	for {
		eval, err := p.lexer.ReadPath()
		if err != nil {
			return err
		}
		if eval.Empty() {
			break
		}
		path := eval.Evaluate(p.env)
		if path == "" {
			return p.lexer.Error("empty path")
		}
		node := p.state.AddNode(path)
		p.state.Defaults = append(p.state.Defaults, node)
	}
	return p.expectToken(T_NEWLINE)
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
		p.subparser.env = graph.NewBindingEnv(p.env)
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
