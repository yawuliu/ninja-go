package builder

import (
	"ninja-go/pkg/util"
	"os"
)

// RealFileSystem 真实文件系统
type RealFileSystem struct{}

func (fs *RealFileSystem) Stat(path string, err *string) int64 {
	//TODO implement me
	panic("implement me")
}

func (fs *RealFileSystem) MakeDir(path string) bool {
	//TODO implement me
	panic("implement me")
}

func (fs *RealFileSystem) WriteFile(path string, contents string, crlf_on_windows bool) bool {
	err := os.WriteFile(path, []byte(contents), 0644)
	if err != nil {
		return false
	}
	return true
}

func (fs *RealFileSystem) RemoveFile(path string) int {
	err := os.RemoveAll(path)
	if err != nil {
		return -1
	}
	return 0
}

func (fs *RealFileSystem) MakeDirs(path string) bool {
	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return false
	}
	return true
}

var _ util.FileSystem = (*RealFileSystem)(nil)

func (fs *RealFileSystem) AllowStatCache(allow bool) bool {
	return false
}

func NewRealFileSystem() *RealFileSystem {
	return &RealFileSystem{}
}

func (fs *RealFileSystem) Open(path string) (util.File, error) {
	return os.Open(path)
}
func (fs *RealFileSystem) Create(path string) (util.File, error) {
	return os.Create(path)
}
func (fs *RealFileSystem) Truncate(name string, size int64) error {
	return os.Truncate(name, size)
}

func (fs *RealFileSystem) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = append(data, 0)
	return data, nil
}

func (fs *RealFileSystem) Remove(path string) error {
	return os.Remove(path)
}

func (fs *RealFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
