package builder

// DyndepFile 存储从一个 dyndep 文件加载的数据，映射边到其动态发现的依赖信息。
type DyndepFile map[*Edge]*Dyndeps

// Dyndeps 存储单个边的动态依赖信息
type Dyndeps struct {
	Used            bool
	Restat          bool
	ImplicitInputs  []*Node
	ImplicitOutputs []*Node
}

func NewDyndeps() *Dyndeps {
	return &Dyndeps{}
}
