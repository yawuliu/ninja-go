package depslog

import (
	"encoding/gob"
	"os"
	"sync"
)

type DepsLog struct {
	mu    sync.RWMutex
	file  string
	cache map[string][]string // output path -> list of implicit dep paths
}

func NewDepsLog(path string) *DepsLog {
	return &DepsLog{
		file:  path,
		cache: make(map[string][]string),
	}
}

// Load 从磁盘读取 .ninja_deps
func (dl *DepsLog) Load() error {
	f, err := os.Open(dl.file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	dec := gob.NewDecoder(f)
	return dec.Decode(&dl.cache)
}

// Save 将缓存写入磁盘
func (dl *DepsLog) Save() error {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	f, err := os.Create(dl.file)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := gob.NewEncoder(f)
	return enc.Encode(dl.cache)
}

// AddDeps 记录某个输出文件的隐式依赖
func (dl *DepsLog) AddDeps(output string, deps []string) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	dl.cache[output] = deps
}

// GetDeps 获取之前记录的隐式依赖
func (dl *DepsLog) GetDeps(output string) []string {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	return dl.cache[output]
}
