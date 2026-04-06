package builder

// Rule 构建规则
type Rule struct {
	Name     string
	Bindings map[string]*EvalString
	Phony    bool
}

func NewRule(name string) *Rule {
	return &Rule{
		Name:     name,
		Bindings: make(map[string]*EvalString),
	}
}

func PhonyRule() *Rule {
	rule := NewRule("phony")
	rule.Phony = true
	return rule
}

func (r *Rule) IsPhony() bool {
	return r.Phony
}

func (r *Rule) AddBinding(key string, val *EvalString) {
	r.Bindings[key] = val
}

func (r *Rule) GetBinding(key string) *EvalString {
	if val, ok := r.Bindings[key]; ok {
		return val
	}
	return nil
}

func IsReservedBinding(key string) bool {
	switch key {
	case "command", "depfile", "dyndep", "description", "deps",
		"generator", "pool", "restat", "rspfile", "rspfile_content", "msvc_deps_prefix":
		return true
	}
	return false
}
