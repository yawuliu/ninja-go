package builder

import (
	"fmt"
	"ninja-go/pkg/util"
)

// Parser 是所有解析器的接口
type Parser interface {
	// Load 加载并解析文件，如果 parent 不为 nil，则使用其 lexer 的上下文
	Load(filename string, parent *BaseParser) error
	// Parse 解析文件内容（由具体解析器实现）
	Parse(filename, input string) error
}

// BaseParser 提供公共字段和方法
type BaseParser struct {
	State      interface{} // 实际类型为 *graph.State，但为避免循环依赖使用 interface{}
	FileReader util.FileSystem
	lexer      *Lexer
}

// NewBaseParser 创建基础解析器
func NewBaseParser(state interface{}, fileReader util.FileSystem) *BaseParser {
	return &BaseParser{
		State:      state,
		FileReader: fileReader,
		lexer:      &Lexer{},
	}
}

// Load 加载文件，如果 parent 非空则使用其 Lexer 进行错误报告
func (b *BaseParser) Load(filename string, parent *BaseParser) error {
	// 读取文件内容
	content, err := b.FileReader.ReadFile(filename)
	if err != nil {
		errMsg := fmt.Sprintf("loading '%s': %v", filename, err)
		if parent != nil {
			parent.lexer.Error(errMsg)
		}
		return fmt.Errorf("%s", errMsg)
	}
	return b.Parse(filename, string(content))
}

// ExpectToken 期望下一个 token 为指定类型，否则返回错误
func (b *BaseParser) ExpectToken(expected Token) error {
	tok := b.lexer.ReadToken()
	if tok != expected {
		msg := fmt.Sprintf("expected %s, got %s%s",
			expected.String(),
			tok.String(),
			TokenErrorHint(expected))
		return b.lexer.Error(msg)
	}
	return nil
}

// Parse 方法需要由具体解析器实现（如 ManifestParser、DyndepParser）
// 这里仅作为占位，实际应在子类型中覆盖
func (b *BaseParser) Parse(filename, input string) error {
	panic("Parse method must be implemented by concrete parser")
}
