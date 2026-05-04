package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

type DirCache map[string]int64 // TimeStamp is int64
type Cache map[string]DirCache

// In the struct definition:
type RealFileSystem struct {
	long_paths_enabled_ bool
	cache               Cache
	useCache            bool
}

func (fs *RealFileSystem) areLongPathsEnabled() bool {
	return fs.long_paths_enabled_
}

func (r *RealFileSystem) Stat(path string, err *string) int64 {
	if runtime.GOOS == "windows" {
		// MSDN: "Naming Files, paths_, and Namespaces"
		const MAX_PATH = 260
		if path != "" && !r.areLongPathsEnabled() && path[0] != '\\' && len(path) > MAX_PATH {
			*err = "Stat(" + path + "): Filename longer than " + string(rune(MAX_PATH)) + " characters"
			return -1
		}
		if !r.useCache {
			return r.StatSingleFile(path, err)
		}

		dir := DirName(path)
		var base string
		if dir != "" {
			base = path[len(dir)+1:]
		} else {
			base = path
		}
		if base == ".." {
			// StatAllFilesInDir does not report any information for base = "..".
			base = "."
			dir = path
		}

		dirLower := strings.ToLower(dir)
		baseLower := strings.ToLower(base)

		if _, ok := r.cache[dirLower]; !ok {
			r.cache[dirLower] = make(map[string]int64)
			val := r.cache[dirLower]
			if !r.StatAllFilesInDir(func() string {
				if dir == "" {
					return "."
				}
				return dir
			}(), &val, err) {
				delete(r.cache, dirLower)
				return -1
			}
		}
		if mtime, ok := r.cache[dirLower][baseLower]; ok {
			return mtime
		}
		return 0
	} else {
		// POSIX
		//var st interface{} // we'll use os.Stat
		info, statErr := os.Stat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return 0
			}
			*err = "stat(" + path + "): " + statErr.Error()
			return -1
		}
		mtime := info.ModTime().UnixNano()
		// Some users set mtime to 0, this should be harmless
		// and avoids conflicting with our return value of 0 meaning that it doesn't exist.
		if mtime == 0 {
			return 1
		}
		return mtime
	}
}

func (fs *RealFileSystem) MakeDir(path string) bool {
	err := os.Mkdir(path, 0755)
	if err != nil {
		if os.IsExist(err) {
			return true
		}
		Error(fmt.Sprintf("mkdir(%s): %v", path, err))
		return false
	}
	return true
}

func (fs *RealFileSystem) WriteFile(path string, contents string, crlf_on_windows bool) bool {
	var flags int
	var perm os.FileMode = 0666
	if runtime.GOOS == "windows" && crlf_on_windows {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	} else {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	var file *os.File
	var err error
	if runtime.GOOS == "windows" && crlf_on_windows {
		// In text mode, Go's os.OpenFile always uses binary mode.
		// We simulate text mode by writing with CRLF replacement.
		// However, to mimic fopen("w"), we simply write and let the OS handle? Actually Go does not have text mode.
		// We'll just write the contents as is; the caller may have converted line endings.
		// For exact behavior, we replace LF with CRLF if not already CRLF.
		// But the original C++ code uses "w" mode which on Windows converts LF to CRLF automatically.
		// To replicate, we can transform the string.
		// For simplicity, assume the caller already did conversion, or we do it here.
		// We'll implement the conversion.
		contents = strings.ReplaceAll(contents, "\n", "\r\n")
	}
	file, err = os.OpenFile(path, flags, perm)
	if err != nil {
		// Error("WriteFile(%s): Unable to create file_. %s", ...) - we ignore the logging function
		return false
	}
	defer file.Close()

	_, err = file.WriteString(contents)
	if err != nil {
		// Error("WriteFile(%s): Unable to write to the file_. %s", ...)
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

func (d *RealFileSystem) MakeDirs(path string) bool {
	dir := DirName(path)
	if dir == "" || dir == "." || dir == "/" {
		// 到达根目录或当前目录，假定已存在
		return true
	}
	var err string
	mtime := d.Stat(dir, &err)
	if mtime < 0 {
		Error("%s", err)
		return false
	}
	if mtime > 0 {
		// 目录已存在
		return true
	}

	// 父目录不存在，先递归创建父目录，再创建当前目录
	if !d.MakeDirs(dir) {
		return false
	}
	if !d.MakeDir(dir) {
		Error(fmt.Sprintf("failed to create directory %s: %v", dir, err))
		return false
	}
	return true
}

var _ FileSystem = (*RealFileSystem)(nil)

func (fs *RealFileSystem) AllowStatCache(allow bool) bool {
	return false
}

func NewRealFileSystem() *RealFileSystem {
	return &RealFileSystem{}
}

func (fs *RealFileSystem) Open(path string) (File, error) {
	return os.Open(path)
}
func (fs *RealFileSystem) Create(path string) (File, error) {
	return os.Create(path)
}
func (fs *RealFileSystem) Truncate(name string, size int64) error {
	return os.Truncate(name, size)
}

func (fs *RealFileSystem) ReadFile(path string, contents *string, err *string) FileReaderStatus {
	_, stat_err := os.Stat(path)
	if stat_err != nil && os.IsNotExist(stat_err) {
		return StatusNotFound
	}
	data, read_err := os.ReadFile(path)
	if read_err != nil {
		return StatusOtherError
	}
	data = append(data, 0)
	*contents = string(data)
	return StatusOkay
}

func (fs *RealFileSystem) Remove(path string) error {
	return os.Remove(path)
}

func (fs *RealFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
