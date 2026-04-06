// Package dyndep 实现 dyndep 文件的解析器
package dyndep

import (
	"fmt"
	"ninja-go/pkg/graph"
	"ninja-go/pkg/parser"
	"ninja-go/pkg/util"
)

// DyndepParser 解析 .dd 文件
type DyndepParser struct {
	lexer      *parser.Lexer
	state      *graph.State
	dyndepFile map[*graph.Edge]*DyndepInfo // 对应 C++ 的 DyndepFile
	env        *parser.BindingEnv          // 当前作用域
	filename   string
}

// NewDyndepParser 创建解析器
func NewDyndepParser(state *graph.State, dyndepFile map[*graph.Edge]*DyndepInfo) *DyndepParser {
	return &DyndepParser{
		state:      state,
		dyndepFile: dyndepFile,
		env:        parser.NewBindingEnv(nil),
	}
}

// Parse 解析内容
func (p *DyndepParser) Parse(filename, content string) error {
	p.filename = filename
	p.lexer = parser.NewLexer(content)
	p.lexer.SetFilename(filename)

	haveVersion := false

	for {
		tok := p.lexer.NextToken()
		switch tok.Type {
		case parser.T_EOF:
			if !haveVersion {
				return p.errorf(tok.Line, "expected 'ninja_dyndep_version = ...'")
			}
			return nil
		case parser.T_ERROR:
			return p.errorf(tok.Line, tok.Value)
		case parser.T_NEWLINE:
			continue
		case parser.T_BUILD:
			if !haveVersion {
				return p.errorf(tok.Line, "expected 'ninja_dyndep_version = ...'")
			}
			if err := p.parseEdge(); err != nil {
				return err
			}
		case parser.T_IDENT:
			p.lexer.UnreadToken()
			if haveVersion {
				return p.errorf(tok.Line, "unexpected identifier")
			}
			if err := p.parseDyndepVersion(); err != nil {
				return err
			}
			haveVersion = true
		default:
			return p.errorf(tok.Line, "unexpected token %v", tok.Type)
		}
	}
}

// parseDyndepVersion 解析 ninja_dyndep_version = ...
func (p *DyndepParser) parseDyndepVersion() error {
	key, err := p.parseLet()
	if err != nil {
		return err
	}
	if key != "ninja_dyndep_version" {
		// 实际上已经由 parseLet 内部检查了，但保留
		return p.errorf(p.lexer.Line(), "expected 'ninja_dyndep_version = ...'")
	}
	// 注意：parseLet 已经将值存入 env，我们需要获取它
	version := p.env.LookupVariable("ninja_dyndep_version")
	major, minor := util.ParseVersion(version)
	if major != 1 || minor != 0 {
		return p.errorf(p.lexer.Line(), "unsupported 'ninja_dyndep_version = %s'", version)
	}
	return nil
}

// parseLet 解析 "key = value" 并存入 env，返回 key
func (p *DyndepParser) parseLet() (string, error) {
	key, err := p.lexer.ReadIdent()
	if err != nil {
		return "", p.errorf(p.lexer.Line(), "expected variable name")
	}
	if !p.lexer.ExpectToken(parser.T_EQUALS) {
		return "", p.errorf(p.lexer.Line(), "expected '='")
	}
	val, err := p.lexer.ReadVarValue()
	if err != nil {
		return "", err
	}
	expanded := val.Evaluate(p.env)
	p.env.AddBinding(key, expanded)
	return key, nil
}

// parseEdge 解析 build 语句
func (p *DyndepParser) parseEdge() error {
	// 1. 读取显式输出（一个）
	out0, err := p.lexer.ReadPath()
	if err != nil {
		return err
	}
	if out0.Empty() {
		return p.errorf(p.lexer.Line(), "expected path")
	}
	path := out0.Evaluate(p.env)
	if path == "" {
		return p.errorf(p.lexer.Line(), "empty path")
	}
	// 规范化路径（保留原样，不加 slash_bits）
	path = parser.NormalizePath(path)
	node := p.state.LookupNode(path)
	if node == nil || node.Edge == nil {
		return p.errorf(p.lexer.Line(), "no build statement exists for '%s'", path)
	}
	edge := node.Edge

	// 检查重复
	if _, ok := p.dyndepFile[edge]; ok {
		return p.errorf(p.lexer.Line(), "multiple statements for '%s'", path)
	}
	dyndeps := &DyndepInfo{}
	p.dyndepFile[edge] = dyndeps

	// 2. 不允许额外的显式输出
	out2, err := p.lexer.ReadPath()
	if err != nil {
		return err
	}
	if !out2.Empty() {
		return p.errorf(p.lexer.Line(), "explicit outputs not supported")
	}

	// 3. 解析隐式输出（如果有 '|'）
	if p.lexer.PeekToken(parser.T_PIPE) {
		p.lexer.NextToken() // consume '|'
		for {
			out, err := p.lexer.ReadPath()
			if err != nil {
				return err
			}
			if out.Empty() {
				break
			}
			outs = append(outs, out)
		}
	}

	// 4. 冒号
	if !p.lexer.ExpectToken(parser.T_COLON) {
		return p.errorf(p.lexer.Line(), "expected ':'")
	}

	// 5. 规则名必须是 "dyndep"
	ruleName, err := p.lexer.ReadIdent()
	if err != nil {
		return err
	}
	if ruleName != "dyndep" {
		return p.errorf(p.lexer.Line(), "expected build command name 'dyndep'")
	}

	// 6. 不允许显式输入
	in2, err := p.lexer.ReadPath()
	if err != nil {
		return err
	}
	if !in2.Empty() {
		return p.errorf(p.lexer.Line(), "explicit inputs not supported")
	}

	// 7. 解析隐式输入（如果有 '|'）
	if p.lexer.PeekToken(parser.T_PIPE) {
		p.lexer.NextToken()
		for {
			in, err := p.lexer.ReadPath()
			if err != nil {
				return err
			}
			if in.Empty() {
				break
			}
			ins = append(ins, in)
		}
	}

	// 8. 不允许 order-only 输入
	if p.lexer.PeekToken(parser.T_PIPE2) {
		return p.errorf(p.lexer.Line(), "order-only inputs not supported")
	}

	// 9. 期望换行
	if !p.lexer.ExpectToken(parser.T_NEWLINE) {
		return p.errorf(p.lexer.Line(), "expected newline")
	}

	// 10. 可选缩进块（restat 绑定）
	if p.lexer.PeekToken(parser.T_INDENT) {
		key, err := p.parseLet()
		if err != nil {
			return err
		}
		if key != "restat" {
			return p.errorf(p.lexer.Line(), "binding is not 'restat'")
		}
		value := p.env.LookupVariable("restat")
		dyndeps.Restat = (value != "")
		// 确保缩进块结束（下一个 token 不是 INDENT）
		if p.lexer.PeekToken(parser.T_INDENT) {
			return p.errorf(p.lexer.Line(), "unexpected indent")
		}
	}

	// 11. 转换隐式输入和输出为节点
	for _, eval := range ins {
		path := eval.Evaluate(p.env)
		if path == "" {
			return p.errorf(p.lexer.Line(), "empty path")
		}
		path = parser.NormalizePath(path)
		n := p.state.AddNode(path)
		dyndeps.ImplicitInputs = append(dyndeps.ImplicitInputs, n)
	}
	for _, eval := range outs {
		path := eval.Evaluate(p.env)
		if path == "" {
			return p.errorf(p.lexer.Line(), "empty path")
		}
		path = parser.NormalizePath(path)
		n := p.state.AddNode(path)
		dyndeps.ImplicitOutputs = append(dyndeps.ImplicitOutputs, n)
	}

	return nil
}

// 辅助错误函数
func (p *DyndepParser) errorf(line int, format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	if line > 0 {
		return fmt.Errorf("%s:%d: %s", p.filename, line, msg)
	}
	return fmt.Errorf("%s: %s", p.filename, msg)
}
