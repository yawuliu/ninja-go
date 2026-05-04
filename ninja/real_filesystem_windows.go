//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"strings"
	"syscall"
	"unsafe"
)

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
		// Open the file_ to get its final path
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
			// Stat the linked file_
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
