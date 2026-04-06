package builder

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
	singleToken string
}

func (es *EvalString) AddText(text string) {
	if len(es.fragments) == 0 && es.singleToken == "" {
		es.singleToken = text
	} else if len(es.fragments) > 0 && es.fragments[len(es.fragments)-1].IsSpecial == false {
		// 合并相邻的文本片段
		es.fragments[len(es.fragments)-1].Text += text
	} else {
		if es.singleToken != "" {
			// 从单 token 转为多片段
			es.fragments = append(es.fragments, EvalFragment{Text: es.singleToken, IsSpecial: false})
			es.singleToken = ""
		}
		es.fragments = append(es.fragments, EvalFragment{Text: text, IsSpecial: false})
	}
}

func (es *EvalString) AddSpecial(name string) {
	if es.singleToken != "" {
		es.fragments = append(es.fragments, EvalFragment{Text: es.singleToken, IsSpecial: false})
		es.singleToken = ""
	}
	es.fragments = append(es.fragments, EvalFragment{Text: name, IsSpecial: true})
}

func (es *EvalString) Empty() bool {
	return len(es.fragments) == 0 && es.singleToken == ""
}

func (es *EvalString) Clear() {
	es.fragments = nil
	es.singleToken = ""
}

func (es *EvalString) Evaluate(env Env) string {
	if len(es.fragments) == 0 {
		return es.singleToken
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
		return es.singleToken
	}
	var sb strings.Builder
	for _, frag := range es.fragments {
		if frag.IsSpecial {
			sb.WriteString("${")
			sb.WriteString(frag.Text)
			sb.WriteString("}")
		} else {
			sb.WriteString(frag.Text)
		}
	}
	return sb.String()
}

// Serialize 返回序列化形式（用于测试）
func (es *EvalString) Serialize() string {
	if len(es.fragments) == 0 && es.singleToken != "" {
		return "[" + es.singleToken + "]"
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
