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
	Open(path string) (File, error)
	Create(path string) (File, error)
	Stat(path string) (os.FileInfo, error)
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	Remove(path string) error
	MkdirAll(path string, perm os.FileMode) error
	Truncate(name string, size int64) error
	MakeDirs(path string) error
}
