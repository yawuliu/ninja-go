// Package dyndep 实现 dyndep 文件的解析器
package main

import (
	"fmt"
)

// DyndepParser 解析 .dd 文件
type DyndepParser struct {
	file_reader_ FileSystem
	lexer_       *Lexer
	state_       *State
	dyndep_file_ *DyndepFile // 对应 C++ 的 DyndepFile
	env_         *BindingEnv // 当前作用域
}

func (b *DyndepParser) Load(filename string, err *string, parent *Lexer) bool {
	// 读取文件内容
	var content string
	status := b.file_reader_.ReadFile(filename, &content, err)
	if status != StatusOkay {
		*err = fmt.Sprintf("loading '%s': %v", filename, err)
		if parent != nil {
			parent.Error(*err, err)
		}
		return false
	}
	return b.Parse(filename, content, err)
}

// Verify that *UserCacher implements Cacher
var _ Parser = (*DyndepParser)(nil)

// NewDyndepParser 创建解析器
func NewDyndepParser(state *State, file_reader FileSystem, dyndepFile *DyndepFile) *DyndepParser {
	return &DyndepParser{
		state_:       state,
		file_reader_: file_reader,
		dyndep_file_: dyndepFile,
		env_:         NewBindingEnv(nil),
	}
}

// Parse 解析 dyndep 文件内容。
func (p *DyndepParser) Parse(filename, input string, err *string) bool {
	p.lexer_.Start(filename, input)

	haveDyndepVersion := false

	for {
		token := p.lexer_.ReadToken()
		switch token {
		case BUILD:
			if !haveDyndepVersion {
				return p.lexer_.Error("expected 'ninja_dyndep_version = ...'", err)
			}
			if !p.parseEdge(err) {
				return false
			}
		case IDENT:
			p.lexer_.UnreadToken()
			if haveDyndepVersion {
				return p.lexer_.Error("unexpected "+token.String(), err)
			}
			if !p.parseDyndepVersion(err) {
				return false
			}
			haveDyndepVersion = true
		case ERROR:
			return p.lexer_.Error(p.lexer_.DescribeLastError(), err)
		case TEOF:
			if !haveDyndepVersion {
				return p.lexer_.Error("expected 'ninja_dyndep_version = ...'", err)
			}
			return true
		case NEWLINE:
			continue
		default:
			return p.lexer_.Error("unexpected "+token.String(), err)
		}
		return false
	}
}

// parseDyndepVersion 解析版本行：ninja_dyndep_version = 1
func (p *DyndepParser) parseDyndepVersion(err *string) bool {
	var name string
	var let_value EvalString
	if !p.parseLet(&name, &let_value, err) {
		return false
	}
	if name != "ninja_dyndep_version" {
		return p.lexer_.Error("expected 'ninja_dyndep_version = ...'", err)
	}

	version := let_value.Evaluate(p.env_)
	major, minor := ParseVersion(version)
	if major != 1 || minor != 0 {
		return p.lexer_.Error(fmt.Sprintf("unsupported 'ninja_dyndep_version = %s'", version), err)
	}
	return true
}

// parseLet 解析 key = value 行
func (p *DyndepParser) parseLet(key *string, value *EvalString, err *string) bool {
	if !p.lexer_.ReadIdent(key) {
		return p.lexer_.Error("expected variable name", err)
	}
	if !p.expectToken(EQUALS, err) {
		return false
	}

	if !p.lexer_.ReadVarValue(value, err) {
		return false
	}
	return true
}

// parseEdge 解析 build 语句
func (p *DyndepParser) parseEdge(err *string) bool {
	// 1. 读取主输出
	out0 := EvalString{}
	if !p.lexer_.ReadPath(&out0, err) {
		return false
	}
	if out0.Empty() {
		return p.lexer_.Error("expected path", err)
	}
	path := out0.Evaluate(p.env_)
	if path == "" {
		return p.lexer_.Error("empty path", err)
	}
	var slash_bits uint64
	CanonicalizePathString(&path, &slash_bits)
	node := p.state_.LookupNode(path)
	if node == nil || node.in_edge() == nil {
		return p.lexer_.Error("no build statement exists for '"+path+"'", err)
	}
	edge := node.in_edge()

	// 检查重复
	if _, ok := (*p.dyndep_file_)[edge]; ok {
		return p.lexer_.Error("multiple statements for '"+path+"'", err)
	}
	info := &Dyndeps{}
	(*p.dyndep_file_)[edge] = info

	// 2. 禁止显式输出
	out := EvalString{}
	if !p.lexer_.ReadPath(&out, err) {
		return false
	}
	if !out.Empty() {
		return p.lexer_.Error("explicit outputs not supported", err)
	}

	// 3. 解析隐式输出（'|' 后）
	var implicitOutputs []*EvalString
	if p.lexer_.PeekToken(PIPE) {
		for {
			if !p.lexer_.ReadPath(&out, err) {
				return false
			}
			if out.Empty() {
				break
			}
			implicitOutputs = append(implicitOutputs, &out)
		}
	}

	// 4. 期望冒号
	if !p.expectToken(COLON, err) {
		return false
	}

	// 5. 规则名必须是 "dyndep"
	var ruleName string
	succ := p.lexer_.ReadIdent(&ruleName)
	if !succ || ruleName != "dyndep" {
		return p.lexer_.Error("expected build command name 'dyndep'", err)
	}

	// 6. 禁止显式输入
	in := EvalString{}
	if !p.lexer_.ReadPath(&in, err) {
		return false
	}
	if !in.Empty() {
		return p.lexer_.Error("explicit inputs not supported", err)
	}

	// 7. 解析隐式输入（'|' 后）
	var implicitInputs []*EvalString
	if p.lexer_.PeekToken(PIPE) {
		for {
			if !p.lexer_.ReadPath(&in, err) {
				return false
			}
			if in.Empty() {
				break
			}
			implicitInputs = append(implicitInputs, &in)
		}
	}

	// 8. 禁止 order-only 输入
	if p.lexer_.PeekToken(PIPE2) {
		return p.lexer_.Error("order-only inputs not supported", err)
	}

	// 9. 期望换行
	if !p.expectToken(NEWLINE, err) {
		return false
	}

	// 10. 可选的缩进块（restat）
	if p.lexer_.PeekToken(INDENT) {
		var key string
		var val EvalString
		if !p.parseLet(&key, &val, err) {
			return false
		}
		if key != "restat" {
			return p.lexer_.Error("binding is not 'restat'", err)
		}
		value := val.Evaluate(p.env_)
		info.Restat = value != ""
	}

	// 11. 将隐式输入转为节点
	for _, inEval := range implicitInputs {
		path := inEval.Evaluate(p.env_)
		if path == "" {
			return p.lexer_.Error("empty path", err)
		}
		var slash_bits uint64
		CanonicalizePathString(&path, &slash_bits)
		node := p.state_.GetNode(path, slash_bits)
		info.ImplicitInputs = append(info.ImplicitInputs, node)
	}

	// 12. 将隐式输出转为节点
	for _, outEval := range implicitOutputs {
		path := outEval.Evaluate(p.env_)
		if path == "" {
			return p.lexer_.Error("empty path", err)
		}
		var slash_bits uint64
		CanonicalizePathString(&path, &slash_bits)
		node := p.state_.GetNode(path, slash_bits)
		info.ImplicitOutputs = append(info.ImplicitOutputs, node)
	}

	return true
}

// expectToken 辅助方法，期望下一个 token 为指定类型
func (p *DyndepParser) expectToken(expected Token, err *string) bool {
	tok := p.lexer_.ReadToken()
	if tok != expected {
		message := "expected " + expected.String()
		message += ", got " + tok.String()
		message += TokenErrorHint(expected)
		return p.lexer_.Error(message, err)
	}
	return true
}
