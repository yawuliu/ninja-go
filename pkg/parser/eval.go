package parser

import "strings"

type EvalFragment struct {
	IsSpecial bool // true 表示变量名，false 表示文本
	Text      string
}

type EvalString struct {
	frags []EvalFragment
}

func (es *EvalString) AddText(s string) {
	if s == "" {
		return
	}
	es.frags = append(es.frags, EvalFragment{IsSpecial: false, Text: s})
}

func (es *EvalString) AddSpecial(name string) {
	es.frags = append(es.frags, EvalFragment{IsSpecial: true, Text: name})
}

func (es *EvalString) Empty() bool {
	return len(es.frags) == 0
}

func (es *EvalString) Evaluate(env *BindingEnv) string {
	var sb strings.Builder
	for _, f := range es.frags {
		if f.IsSpecial {
			sb.WriteString(env.LookupVariable(f.Text))
		} else {
			sb.WriteString(f.Text)
		}
	}
	return sb.String()
}

type BindingEnv struct {
	parent *BindingEnv
	vars   map[string]string
}

func NewBindingEnv(parent *BindingEnv) *BindingEnv {
	return &BindingEnv{
		parent: parent,
		vars:   make(map[string]string),
	}
}

func (e *BindingEnv) AddBinding(key, value string) {
	e.vars[key] = value
}

func (e *BindingEnv) LookupVariable(name string) string {
	if val, ok := e.vars[name]; ok {
		return val
	}
	if e.parent != nil {
		return e.parent.LookupVariable(name)
	}
	return ""
}

func (e *BindingEnv) LookupRule(name string) interface{} {
	// 暂未实现规则查找，可后续扩展
	return nil
}
