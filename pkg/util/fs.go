package util

import (
	"io"
	"os"
)

type File interface {
	io.ReadWriteCloser
	Stat() (os.FileInfo, error)
	WriteString(s string) (n int, err error)
}

type FileSystem interface {
	Stat(path string, err *string) int64
	MakeDir(path string) bool
	WriteFile(path string, contents string, crlf_on_windows bool) bool
	RemoveFile(path string) int
	MakeDirs(path string) bool

	Open(path string) (File, error)
	Create(path string) (File, error)
	ReadFile(path string) ([]byte, error)
	Truncate(name string, size int64) error
	AllowStatCache(allow bool) bool
}
