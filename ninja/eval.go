package main

import (
	"strings"
)

type EvalFragment struct {
	IsSpecial bool // true 表示变量名，false 表示文本
	Text      string
}

type EvalString struct {
	fragments []EvalFragment
	// 优化：如果只有一个普通文本片段，直接存储在此
	single_token_ string
}

func (es *EvalString) AddText(text string) {
	if len(es.fragments) == 0 && es.single_token_ == "" {
		es.single_token_ = text
	} else if len(es.fragments) > 0 && es.fragments[len(es.fragments)-1].IsSpecial == false {
		// 合并相邻的文本片段
		es.fragments[len(es.fragments)-1].Text += text
	} else {
		if es.single_token_ != "" {
			// 从单 token 转为多片段
			es.fragments = append(es.fragments, EvalFragment{Text: es.single_token_, IsSpecial: false})
			es.single_token_ = ""
		}
		es.fragments = append(es.fragments, EvalFragment{Text: text, IsSpecial: false})
	}
}

func (es *EvalString) AddSpecial(name string) {
	if es.single_token_ != "" {
		es.fragments = append(es.fragments, EvalFragment{Text: es.single_token_, IsSpecial: false})
		es.single_token_ = ""
	}
	es.fragments = append(es.fragments, EvalFragment{Text: name, IsSpecial: true})
}

func (es *EvalString) Empty() bool {
	return len(es.fragments) == 0 && es.single_token_ == ""
}

func (es *EvalString) Clear() {
	es.fragments = nil
	es.single_token_ = ""
}

func (es *EvalString) Evaluate(env Env) string {
	if len(es.fragments) == 0 {
		return es.single_token_
	}
	var sb strings.Builder
	for _, frag := range es.fragments {
		if frag.IsSpecial {
			sb.WriteString(env.LookupVariable(frag.Text))
		} else {
			sb.WriteString(frag.Text)
		}
	}
	return sb.String()
}

// Unparse 返回未展开的原始字符串表示（用于调试）
func (es *EvalString) Unparse() string {
	if len(es.fragments) == 0 {
		return strings.ReplaceAll(es.single_token_, "$", "$$")
	}
	var sb strings.Builder
	for _, frag := range es.fragments {
		if frag.IsSpecial {
			// 使用 $var 格式而不是 ${var}
			sb.WriteString("$")
			sb.WriteString(frag.Text)
		} else {
			// Escape $ as $$ in text fragments
			sb.WriteString(strings.ReplaceAll(frag.Text, "$", "$$"))
		}
	}
	return sb.String()
}

// Serialize 返回序列化形式（用于测试）
func (es *EvalString) Serialize() string {
	if len(es.fragments) == 0 && es.single_token_ != "" {
		return "[" + es.single_token_ + "]"
	}
	var sb strings.Builder
	for _, frag := range es.fragments {
		sb.WriteString("[")
		if frag.IsSpecial {
			sb.WriteString("$")
		}
		sb.WriteString(frag.Text)
		sb.WriteString("]")
	}
	return sb.String()
}
