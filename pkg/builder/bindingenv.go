package builder

// Env 变量查找接口
type Env interface {
	LookupVariable(varName string) string
}

type BindingEnv struct {
	parent *BindingEnv
	vars   map[string]string
	rules  map[string]*Rule
}

func NewBindingEnv(parent *BindingEnv) *BindingEnv {
	return &BindingEnv{
		parent: parent,
		vars:   make(map[string]string),
		rules:  make(map[string]*Rule),
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

func (e *BindingEnv) AddRule(rule *Rule) {
	// 当前作用域中不能有重名规则
	if _, ok := e.rules[rule.Name]; ok {
		return
	}
	e.rules[rule.Name] = rule
}

func (e *BindingEnv) LookupRule(ruleName string) *Rule {
	if r, ok := e.rules[ruleName]; ok {
		return r
	}
	if e.parent != nil {
		return e.parent.LookupRule(ruleName)
	}
	return nil
}

func (e *BindingEnv) LookupRuleCurrentScope(ruleName string) *Rule {
	return e.rules[ruleName]
}

func (e *BindingEnv) GetRules() map[string]*Rule {
	return e.rules
}

// LookupWithFallback 用于边中的变量查找：先查边自身绑定，再查规则绑定的求值结果，最后查父作用域
func (e *BindingEnv) LookupWithFallback(varName string, eval *EvalString, env Env) string {
	if val, ok := e.vars[varName]; ok {
		return val
	}
	if eval != nil {
		return eval.Evaluate(env)
	}
	if e.parent != nil {
		return e.parent.LookupVariable(varName)
	}
	return ""
}
