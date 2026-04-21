package builder

import (
	"golang.org/x/sys/windows"
	"ninja-go/pkg/util"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
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
		// MSDN: "Naming Files, Paths, and Namespaces"
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
	//TODO implement me
	panic("implement me")
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
		// Error("WriteFile(%s): Unable to create file. %s", ...) - we ignore the logging function
		return false
	}
	defer file.Close()

	_, err = file.WriteString(contents)
	if err != nil {
		// Error("WriteFile(%s): Unable to write to the file. %s", ...)
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

func (fs *RealFileSystem) StatSingleFile(path string, err *string) int64 {
	var attrs syscall.Win32FileAttributeData
	// Call GetFileAttributesEx
	pathPtr, _ := syscall.UTF16PtrFromString(path)
	if errGet := windows.GetFileAttributesEx(pathPtr, syscall.GetFileExInfoStandard, (*byte)(unsafe.Pointer(&attrs))); errGet != nil {
		if errGet == syscall.ERROR_FILE_NOT_FOUND || errGet == syscall.ERROR_PATH_NOT_FOUND {
			return 0
		}
		*err = "GetFileAttributesEx(" + path + "): " + errGet.Error()
		return -1
	}

	// Check for reparse point
	if attrs.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		// Open the file to get its final path
		pathPtr, _ := syscall.UTF16PtrFromString(path)
		handle, errOpen := syscall.CreateFile(pathPtr, 0, 0, nil, syscall.OPEN_EXISTING,
			syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if errOpen != nil {
			if errOpen == syscall.ERROR_FILE_NOT_FOUND || errOpen == syscall.ERROR_PATH_NOT_FOUND {
				return 0
			}
			*err = "CreateFile(" + path + "): " + errOpen.Error()
			if handle != syscall.InvalidHandle {
				syscall.CloseHandle(handle)
			}
			return -1
		}
		defer syscall.CloseHandle(handle)

		// Get final path by handle
		const maxPath = 260 // MAX_PATH
		buf := make([]uint16, maxPath)
		const FILE_NAME_NORMALIZED = 0x0
		len, errFinal := windows.GetFinalPathNameByHandle(windows.Handle(handle), &buf[0], uint32(len(buf)), FILE_NAME_NORMALIZED)
		if errFinal != nil {
			if errFinal == syscall.ERROR_FILE_NOT_FOUND || errFinal == syscall.ERROR_PATH_NOT_FOUND {
				return 0
			}
			*err = "GetFinalPathNameByHandle(" + path + "): " + errFinal.Error()
			return -1
		}
		if len == 0 {
			*err = "GetFinalPathNameByHandle(" + path + "): returned zero length"
			return -1
		}
		finalPath := syscall.UTF16ToString(buf[:len])
		return fs.StatSingleFile(finalPath, err)
	}

	// Convert FILETIME to nanoseconds
	return TimeStampFromFileTime(attrs.LastWriteTime)
}

func DirName(path string) string {
	var sepSet string
	if runtime.GOOS == "windows" {
		sepSet = "\\/"
	} else {
		sepSet = "/"
	}

	// Find last separator
	i := strings.LastIndexAny(path, sepSet)
	if i == -1 {
		return ""
	}
	// Skip over any consecutive separators before i
	for i > 0 && strings.ContainsAny(path[i-1:i], sepSet) {
		i--
	}
	return path[:i]
}
func (fs *RealFileSystem) StatAllFilesInDir(dir string, stamps *DirCache, err *string) bool {
	// Build search pattern: dir\*
	pattern := dir + `\*`
	patternPtr, errUtf16 := syscall.UTF16PtrFromString(pattern)
	if errUtf16 != nil {
		*err = errUtf16.Error()
		return false
	}

	var ffd syscall.Win32finddata
	handle, findErr := syscall.FindFirstFile(patternPtr, &ffd)
	if findErr != nil {
		// If directory doesn't exist or is empty, treat as success.
		if findErr == syscall.ERROR_FILE_NOT_FOUND ||
			findErr == syscall.ERROR_PATH_NOT_FOUND ||
			findErr == windows.ERROR_DIRECTORY {
			return true
		}
		*err = "FindFirstFile(" + dir + "): " + findErr.Error()
		return false
	}
	defer syscall.FindClose(handle)
	var lowername string
	var mtime int64
	for {
		name := syscall.UTF16ToString(ffd.FileName[:])
		// Skip ".." (and optionally "." if desired; original skips only "..")
		if name == ".." {
			goto next
		}
		lowername = strings.ToLower(name)
		mtime = 0
		// Check for reparse point (symlink)
		if ffd.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			// Stat the linked file
			fullPath := dir + `\` + name
			mtime = fs.StatSingleFile(fullPath, err)
			if mtime == -1 {
				return false
			}
		} else {
			mtime = TimeStampFromFileTime(ffd.LastWriteTime)
		}
		(*stamps)[lowername] = mtime

	next:
		if errNext := syscall.FindNextFile(handle, &ffd); errNext != nil {
			if errNext == syscall.ERROR_NO_MORE_FILES {
				break
			}
			*err = "FindNextFile(" + dir + "): " + errNext.Error()
			return false
		}
	}
	return true
}

func TimeStampFromFileTime(filetime syscall.Filetime) int64 {
	// Combine low and high parts into a 64-bit value representing 100-ns intervals.
	mtime := (uint64(filetime.HighDateTime) << 32) | uint64(filetime.LowDateTime)

	// 12622770400 seconds * (1e9 ns / 100) = 12622770400 * 10^7 = 126227704000000000
	const adjust = 12622770400 * (1000000000 / 100) // = 126227704000000000
	return int64(mtime - adjust)
}
