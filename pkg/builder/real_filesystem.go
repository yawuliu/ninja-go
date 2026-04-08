package builder

import (
	"ninja-go/pkg/util"
	"os"
)

// RealFileSystem 真实文件系统
type RealFileSystem struct{}

var _ util.FileSystem = (*RealFileSystem)(nil)

func (fs *RealFileSystem) AllowStatCache(allow bool) bool {
	return false
}

func (fs *RealFileSystem) MakeDirs(path string) error {
	return os.MkdirAll(path, os.ModePerm)
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

func (fs *RealFileSystem) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (fs *RealFileSystem) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = append(data, 0)
	return data, nil
}

func (fs *RealFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (fs *RealFileSystem) Remove(path string) error {
	return os.Remove(path)
}

func (fs *RealFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
