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

func (fs *RealFileSystem) ReadFile(path string, contents *string, err *string) util.FileReaderStatus {
	_, stat_err := os.Stat(path)
	if stat_err != nil && os.IsNotExist(stat_err) {
		return util.StatusNotFound
	}
	data, read_err := os.ReadFile(path)
	if read_err != nil {
		return util.StatusOtherError
	}
	data = append(data, 0)
	*contents = string(data)
	return util.StatusOkay
}

func (fs *RealFileSystem) Remove(path string) error {
	return os.Remove(path)
}

func (fs *RealFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
