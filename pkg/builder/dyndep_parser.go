// Package dyndep 实现 dyndep 文件的解析器
package builder

import (
	"fmt"
	"ninja-go/pkg/util"
)

// DyndepParser 解析 .dd 文件
type DyndepParser struct {
	fileReader util.FileSystem
	lexer      *Lexer
	state      *State
	dyndepFile *DyndepFile // 对应 C++ 的 DyndepFile
	env        *BindingEnv // 当前作用域
	filename   string
}

func (b *DyndepParser) Load(filename string, parent *BaseParser) error {
	// 读取文件内容
	content, err := b.fileReader.ReadFile(filename)
	if err != nil {
		errMsg := fmt.Sprintf("loading '%s': %v", filename, err)
		if parent != nil {
			parent.lexer.Error(errMsg)
		}
		return fmt.Errorf("%s", errMsg)
	}
	return b.Parse(filename, string(content))
}

// Verify that *UserCacher implements Cacher
var _ Parser = (*DyndepParser)(nil)

// NewDyndepParser 创建解析器
func NewDyndepParser(state *State, file_reader util.FileSystem, dyndepFile *DyndepFile) *DyndepParser {
	return &DyndepParser{
		state:      state,
		fileReader: file_reader,
		dyndepFile: dyndepFile,
		env:        NewBindingEnv(nil),
	}
}

// Parse 解析 dyndep 文件内容。
func (p *DyndepParser) Parse(filename, input string) error {
	p.lexer.Start(filename, input)

	haveVersion := false

	for {
		token := p.lexer.ReadToken()
		switch token {
		case T_BUILD:
			if !haveVersion {
				return p.lexer.Error("expected 'ninja_dyndep_version = ...'")
			}
			if err := p.parseEdge(); err != nil {
				return err
			}
		case T_IDENT:
			p.lexer.UnreadToken()
			if haveVersion {
				return p.lexer.Error("unexpected " + TokenName(token))
			}
			if err := p.parseDyndepVersion(); err != nil {
				return err
			}
			haveVersion = true
		case T_ERROR:
			return p.lexer.Error(p.lexer.DescribeLastError())
		case T_EOF:
			if !haveVersion {
				return p.lexer.Error("expected 'ninja_dyndep_version = ...'")
			}
			return nil
		case T_NEWLINE:
			continue
		default:
			return p.lexer.Error("unexpected " + TokenName(token))
		}
	}
}

// parseDyndepVersion 解析版本行：ninja_dyndep_version = 1
func (p *DyndepParser) parseDyndepVersion() error {
	key, value, err := p.parseLet()
	if err != nil {
		return err
	}
	if key != "ninja_dyndep_version" {
		return p.lexer.Error("expected 'ninja_dyndep_version = ...'")
	}
	version := value.Evaluate(p.env)
	major, minor := util.ParseVersion(version)
	if major != 1 || minor != 0 {
		return p.lexer.Error(fmt.Sprintf("unsupported 'ninja_dyndep_version = %s'", version))
	}
	return nil
}

// parseLet 解析 key = value 行
func (p *DyndepParser) parseLet() (string, *EvalString, error) {
	key, err := p.lexer.ReadIdent()
	if err != nil {
		return "", nil, p.lexer.Error("expected variable name")
	}
	if err := p.expectToken(T_EQUALS); err != nil {
		return "", nil, err
	}
	value, err := p.lexer.ReadVarValue()
	if err != nil {
		return "", nil, err
	}
	return key, value, nil
}

// parseEdge 解析 build 语句
func (p *DyndepParser) parseEdge() error {
	// 1. 读取主输出
	out0, err := p.lexer.ReadPath()
	if err != nil {
		return err
	}
	if out0.Empty() {
		return p.lexer.Error("expected path")
	}
	path := out0.Evaluate(p.env)
	if path == "" {
		return p.lexer.Error("empty path")
	}
	norm, _ := util.CanonicalizePath(path)
	node := p.state.LookupNode(norm)
	if node == nil || node.InEdge == nil {
		return p.lexer.Error("no build statement exists for '" + norm + "'")
	}
	edge := node.InEdge

	// 检查重复
	if _, ok := (*p.dyndepFile)[edge]; ok {
		return p.lexer.Error("multiple statements for '" + norm + "'")
	}
	info := &DyndepInfo{}
	(*p.dyndepFile)[edge] = info

	// 2. 禁止显式输出
	out, err := p.lexer.ReadPath()
	if err != nil {
		return err
	}
	if !out.Empty() {
		return p.lexer.Error("explicit outputs not supported")
	}

	// 3. 解析隐式输出（'|' 后）
	var implicitOutputs []*EvalString
	if p.lexer.PeekToken(T_PIPE) {
		for {
			out, err := p.lexer.ReadPath()
			if err != nil {
				return err
			}
			if out.Empty() {
				break
			}
			implicitOutputs = append(implicitOutputs, out)
		}
	}

	// 4. 期望冒号
	if err := p.expectToken(T_COLON); err != nil {
		return err
	}

	// 5. 规则名必须是 "dyndep"
	ruleName, err := p.lexer.ReadIdent()
	if err != nil || ruleName != "dyndep" {
		return p.lexer.Error("expected build command name 'dyndep'")
	}

	// 6. 禁止显式输入
	in, err := p.lexer.ReadPath()
	if err != nil {
		return err
	}
	if !in.Empty() {
		return p.lexer.Error("explicit inputs not supported")
	}

	// 7. 解析隐式输入（'|' 后）
	var implicitInputs []*EvalString
	if p.lexer.PeekToken(T_PIPE) {
		for {
			in, err := p.lexer.ReadPath()
			if err != nil {
				return err
			}
			if in.Empty() {
				break
			}
			implicitInputs = append(implicitInputs, in)
		}
	}

	// 8. 禁止 order-only 输入
	if p.lexer.PeekToken(T_PIPE2) {
		return p.lexer.Error("order-only inputs not supported")
	}

	// 9. 期望换行
	if err := p.expectToken(T_NEWLINE); err != nil {
		return err
	}

	// 10. 可选的缩进块（restat）
	if p.lexer.PeekToken(T_INDENT) {
		key, val, err := p.parseLet()
		if err != nil {
			return err
		}
		if key != "restat" {
			return p.lexer.Error("binding is not 'restat'")
		}
		value := val.Evaluate(p.env)
		info.Restat = value != ""
	}

	// 11. 将隐式输入转为节点
	for _, inEval := range implicitInputs {
		path := inEval.Evaluate(p.env)
		if path == "" {
			return p.lexer.Error("empty path")
		}
		norm, slashBits := util.CanonicalizePath(path)
		node := p.state.GetNode(norm, slashBits)
		info.ImplicitInputs = append(info.ImplicitInputs, node)
	}

	// 12. 将隐式输出转为节点
	for _, outEval := range implicitOutputs {
		path := outEval.Evaluate(p.env)
		if path == "" {
			return p.lexer.Error("empty path")
		}
		norm, slashBits := util.CanonicalizePath(path)
		node := p.state.GetNode(norm, slashBits)
		info.ImplicitOutputs = append(info.ImplicitOutputs, node)
	}

	return nil
}

// expectToken 辅助方法，期望下一个 token 为指定类型
func (p *DyndepParser) expectToken(expected Token) error {
	tok := p.lexer.ReadToken()
	if tok != expected {
		return p.lexer.Error(fmt.Sprintf("expected %s, got %s", TokenName(expected), TokenName(tok)))
	}
	return nil
}
