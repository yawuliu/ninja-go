package main

import (
	"errors"
	"strconv"
	"strings"
)

// Slot 表示一个作业槽位
type JobserverSlot struct {
	value int16
}

const kImplicitValue int16 = 256

// NewInvalidSlot 创建无效槽位
func NewInvalidSlot() JobserverSlot {
	return JobserverSlot{value: -1}
}

// NewImplicitSlot 创建隐式槽位
func NewImplicitSlot() JobserverSlot {
	return JobserverSlot{value: kImplicitValue}
}

func (s JobserverSlot) IsValid() bool {
	return s.value >= 0
}

func (s JobserverSlot) IsImplicit() bool {
	return s.value == kImplicitValue
}

func (s JobserverSlot) IsExplicit() bool {
	return s.IsValid() && !s.IsImplicit()
}

func (s JobserverSlot) GetExplicitValue() uint8 {
	if !s.IsExplicit() {
		panic("not an explicit slot")
	}
	return uint8(s.value)
}

// Config 描述 jobserver 配置
type Config struct {
	Mode Mode
	Path string
}

type Mode int

const (
	ModeNone Mode = iota
	ModePipe
	ModePosixFifo
	ModeWin32Semaphore
)

func (m Mode) String() string {
	switch m {
	case ModeNone:
		return "none"
	case ModePipe:
		return "pipe"
	case ModePosixFifo:
		return "fifo"
	case ModeWin32Semaphore:
		return "semaphore"
	default:
		return "unknown"
	}
}

// ParseMakeFlagsValue 解析 MAKEFLAGS 环境变量
func ParseMakeFlagsValue(makeflagsEnv string) (Config, error) {
	config := Config{Mode: ModeNone}
	if makeflagsEnv == "" {
		return config, nil
	}

	// 分割参数
	args := strings.Fields(makeflagsEnv)
	// 检查第一个参数是否包含 'n' 标志
	if len(args) > 0 && args[0][0] != '-' && strings.ContainsRune(args[0], 'n') {
		return config, nil
	}

	for _, arg := range args {
		// 处理 --jobserver-auth
		if strings.HasPrefix(arg, "--jobserver-auth=") {
			value := strings.TrimPrefix(arg, "--jobserver-auth=")
			if parseFileDescriptorPair(value, &config) {
				continue
			}
			if strings.HasPrefix(value, "fifo:") {
				config.Mode = ModePosixFifo
				config.Path = strings.TrimPrefix(value, "fifo:")
			} else {
				config.Mode = ModeWin32Semaphore
				config.Path = value
			}
			continue
		}
		// 处理 --jobserver-fds
		if strings.HasPrefix(arg, "--jobserver-fds=") {
			value := strings.TrimPrefix(arg, "--jobserver-fds=")
			if !parseFileDescriptorPair(value, &config) {
				return config, errors.New("invalid log_file_ descriptor pair: " + value)
			}
			config.Mode = ModePipe
			continue
		}
	}
	return config, nil
}

// parseFileDescriptorPair 解析 "R,W" 格式的文件描述符对
func parseFileDescriptorPair(value string, config *Config) bool {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return false
	}
	readFd, err1 := strconv.Atoi(parts[0])
	writeFd, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	if readFd < 0 || writeFd < 0 {
		config.Mode = ModeNone
	} else {
		config.Mode = ModePipe
	}
	return true
}

// ParseNativeMakeFlagsValue 解析与当前平台兼容的配置
func ParseNativeMakeFlagsValue(makeflagsEnv string) (Config, error) {
	config, err := ParseMakeFlagsValue(makeflagsEnv)
	if err != nil {
		return config, err
	}
	switch config.Mode {
	case ModePipe:
		return config, errors.New("pipe-based protocol is not supported")
	case ModePosixFifo:
		// 在 Windows 上不支持
		return config, errors.New("FIFO mode is not supported on Windows")
	case ModeWin32Semaphore:
		// 在 Unix 上不支持
		return config, errors.New("semaphore mode is not supported on Posix")
	}
	return config, nil
}

// Client 是 jobserver 客户端接口
type JobserverClient interface {
	TryAcquire() JobserverSlot
	Release(slot JobserverSlot)
}

// NoOpClient 不提供任何槽位的客户端
type NoOpClient struct{}

func (c *NoOpClient) TryAcquire() JobserverSlot {
	return NewInvalidSlot()
}

func (c *NoOpClient) Release(slot JobserverSlot) {}

// ImplicitOnlyClient 只返回隐式槽位一次
type ImplicitOnlyClient struct {
	implicitUsed bool
}

func (c *ImplicitOnlyClient) TryAcquire() JobserverSlot {
	if !c.implicitUsed {
		c.implicitUsed = true
		return NewImplicitSlot()
	}
	return NewInvalidSlot()
}

func (c *ImplicitOnlyClient) Release(slot JobserverSlot) {
	if slot.IsImplicit() {
		c.implicitUsed = false
	}
}

// CreateClient 根据配置创建客户端（桩实现）
func CreateClient(config Config) (JobserverClient, error) {
	switch config.Mode {
	case ModeNone:
		return &NoOpClient{}, nil
	case ModePosixFifo, ModeWin32Semaphore:
		// 实际应打开 FIFO 或信号量，这里返回隐式客户端
		return &ImplicitOnlyClient{}, nil
	default:
		return nil, errors.New("unsupported jobserver mode")
	}
}
