package main

import (
	"fmt"
)

// Parser 是所有解析器的接口
type Parser interface {
	// Load 加载并解析文件，如果 parent 不为 nil，则使用其 lexer 的上下文
	Load(filename string, err *string, parent *Lexer) bool
	// Parse 解析文件内容（由具体解析器实现）
	Parse(filename, input string, err *string) bool
}

// BaseParser 提供公共字段和方法
type BaseParser struct {
	State      interface{} // 实际类型为 *graph.State，但为避免循环依赖使用 interface{}
	FileReader FileSystem
	lexer      *Lexer
}

// NewBaseParser 创建基础解析器
func NewBaseParser(state interface{}, fileReader FileSystem) *BaseParser {
	return &BaseParser{
		State:      state,
		FileReader: fileReader,
		lexer:      &Lexer{},
	}
}

// Load 加载文件，如果 parent 非空则使用其 Lexer 进行错误报告
func (b *BaseParser) Load(filename string, err *string, parent *Lexer) bool {
	var content string
	status := b.FileReader.ReadFile(filename, &content, err)
	if status != StatusOkay {
		*err = fmt.Sprintf("loading '%s': %s", filename, *err)
		if parent != nil {
			parent.Error(*err, err)
		}
		return false
	}
	return b.Parse(filename, content, err)
}

// ExpectToken 期望下一个 token 为指定类型，否则返回错误
func (b *BaseParser) ExpectToken(expected Token, err *string) bool {
	tok := b.lexer.ReadToken()
	if tok != expected {
		message := "expected " + expected.String()
		message += ", got " + tok.String()
		message += TokenErrorHint(expected)
		return b.lexer.Error(message, err)
	}
	return true
}

// Parse 方法需要由具体解析器实现（如 ManifestParser、DyndepParser）
// 这里仅作为占位，实际应在子类型中覆盖
func (b *BaseParser) Parse(filename, input string, err *string) bool {
	panic("Parse method must be implemented by concrete parser")
}
