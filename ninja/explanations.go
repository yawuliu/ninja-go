package main

import (
	"fmt"
)

// Explanations 记录与某个 item 相关的解释字符串（用于 -d explain）
type Explanations struct {
	// map 的键可以是任何类型的指针（或可比较的值），这里使用 interface{} 以允许任意 key
	records map[interface{}][]string
}

// NewExplanations 创建一个新的 Explanations 实例
func NewExplanations() *Explanations {
	return &Explanations{
		records: make(map[interface{}][]string),
	}
}

// Record 记录一条解释信息（格式化）
func (e *Explanations) Record(item interface{}, format string, args ...interface{}) {
	if e == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	e.records[item] = append(e.records[item], msg)
}

// RecordArgs 记录一条解释信息（接收已格式化的字符串，或者使用可变参数，与 Record 合并）
// 为了接口一致性，这里直接调用 Record
func (e *Explanations) RecordArgs(item interface{}, format string, args ...interface{}) {
	e.Record(item, format, args...)
}

// LookupAndAppend 查找与 item 相关的解释信息，并追加到 out 切片中
func (e *Explanations) LookupAndAppend(item interface{}, out *[]string) {
	if e == nil {
		return
	}
	if list, ok := e.records[item]; ok {
		*out = append(*out, list...)
	}
}

// OptionalExplanations 包装 Explanations 指针，允许 nil 值
type OptionalExplanations struct {
	expl *Explanations
}

// Verify that *UserCacher implements Cacher
// var _ *Explanations = (*OptionalExplanations)(nil)

// NewOptionalExplanations 创建可选的 Explanations 包装器
func NewOptionalExplanations(expl *Explanations) OptionalExplanations {
	return OptionalExplanations{expl: expl}
}

// Record 如果内部指针非空，则记录解释信息
func (o *OptionalExplanations) Record(item interface{}, format string, args ...interface{}) {
	if o == nil || o.expl == nil {
		return
	}
	o.expl.Record(item, format, args...)
}

// RecordArgs 同上
func (o *OptionalExplanations) RecordArgs(item interface{}, format string, args ...interface{}) {
	if o == nil || o.expl == nil {
		return
	}
	o.expl.RecordArgs(item, format, args...)
}

// LookupAndAppend 如果内部指针非空，则查找并追加
func (o *OptionalExplanations) LookupAndAppend(item interface{}, out *[]string) {
	if o == nil || o.expl == nil {
		return
	}
	o.expl.LookupAndAppend(item, out)
}

// Ptr 返回内部的 Explanations 指针（可能为 nil）
func (o OptionalExplanations) Ptr() *Explanations {
	return o.expl
}
