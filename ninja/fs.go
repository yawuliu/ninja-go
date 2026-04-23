package main

import (
	"io"
	"os"
)

type File interface {
	io.ReadWriteCloser
	Stat() (os.FileInfo, error)
	WriteString(s string) (n int, err error)
}
type FileReaderStatus int

const (
	StatusOkay FileReaderStatus = iota
	StatusNotFound
	StatusOtherError
)

type FileSystem interface {
	Stat(path string, err *string) int64
	MakeDir(path string) bool
	WriteFile(path string, contents string, crlf_on_windows bool) bool
	RemoveFile(path string) int
	MakeDirs(path string) bool

	Open(path string) (File, error)
	Create(path string) (File, error)
	ReadFile(path string, contents *string, err *string) FileReaderStatus
	Truncate(name string, size int64) error
	AllowStatCache(allow bool) bool
}
