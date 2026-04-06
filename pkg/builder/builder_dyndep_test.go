package builder

import (
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDyndepWithProto(t *testing.T) {
	// 切换到测试目录
	testDir := "testdata/dyndep"
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Skip("testdata/dyndep directory not found")
	}

	// 切换到测试目录
	cwd, err := os.Getwd()
	assert.NoError(t, err)
	defer os.Chdir(cwd)
	err = os.Chdir(testDir)
	assert.NoError(t, err)

	// 确保 fake_protoc 脚本可执行（Unix）
	if runtime.GOOS != "windows" {
		if err := os.Chmod("fake_protoc.sh", 0755); err != nil {
			t.Skipf("cannot chmod fake_protoc.sh: %v", err)
		}
	}

	// 清理可能遗留的生成文件
	filesToClean := []string{
		"proto.pb.cc", "proto.pb.h", "proto.pb.dd", "proto.pb.o",
	}
	for _, f := range filesToClean {
		os.Remove(f)
	}

	// 创建状态并解析 build.ninja
	state := NewState()
	p := NewParser(state)
	err = p.ParseFile("build.ninja")
	assert.NoError(t, err)

	// 创建 builder（使用顺序执行避免并发复杂性）
	// 注意：为了测试简单，我们可以暂时使用顺序 builder，或者使用现有的并行 builder 但确保两阶段正确。
	// 这里直接使用我们刚才修改的 Builder（支持两阶段），但需要传入合适的 parallel（例如1）。
	bld := NewBuilder(state, 1, nil, nil) // 顺序执行方便调试

	// 构建默认目标
	err = bld.Build([]string{"default"})
	assert.NoError(t, err)

	// 验证生成的文件是否存在
	for _, f := range []string{"proto.pb.cc", "proto.pb.h", "proto.pb.o"} {
		_, err := os.Stat(f)
		assert.NoError(t, err, "missing %s", f)
	}

	// 验证 dyndep 文件被正确解析并添加了新的边
	// 由于我们调用了 bld.runEdges（并行），但实际上两阶段后应该包含了 proto.pb.o 的边
	// 简单验证 state 中是否包含 proto.pb.o 对应的边
	var found bool
	for _, edge := range state.Edges {
		for _, out := range edge.Outputs {
			if out.Path == "proto.pb.o" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "dyndep edge for proto.pb.o not added")
}
