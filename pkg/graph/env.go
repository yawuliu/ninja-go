package graph

// Env 变量查找接口
type Env interface {
	LookupVariable(varName string) string
}
