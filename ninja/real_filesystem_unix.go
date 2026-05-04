//go:build !windows

package main

func (fs *RealFileSystem) StatSingleFile(path string, err *string) int64 {
	*err = "StatSingleFile not implemented on this platform"
	return -1
}

func (fs *RealFileSystem) StatAllFilesInDir(dir string, stamps *DirCache, err *string) bool {
	*err = "StatAllFilesInDir not implemented on this platform"
	return false
}

func TimeStampFromFileTime(lo, hi uint32) int64 { return 0 }
