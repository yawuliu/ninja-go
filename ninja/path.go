package main

import (
	"path/filepath"
	"runtime"
	"strings"
)

// NormalizePath 统一使用正斜杠
func NormalizePath(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(p, "\\", "/")
	}
	return p
}

// AbsPath 获取绝对路径并规范化
func AbsPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return NormalizePath(abs), nil
}
