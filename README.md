# ninja-go

Go 语言移植的 [Ninja](https://ninja-build.org/) 构建系统。

原始 C++ 实现在 `ninja-cpp/` 目录中，`ninja/` 是 Go 移植版本。

## 快速开始

```bash
# 构建
go build -o ninja-go.exe ./ninja/

# 运行
./ninja-go.exe -C /path/to/build/dir

# 或直接在当前目录（需有 build.ninja）
./ninja-go.exe
```

## 命令行选项

```
usage: ninja [options] [targets...]

options:
  --version      print ninja version
  -v, --verbose  show all command lines while building
  --quiet        don't show progress status, just command output
  -C DIR         change to DIR before doing anything else
  -f FILE        specify input build file [default=build.ninja]
  -j N           run N jobs in parallel (0 means infinity)
  -k N           keep going until N jobs fail (0 means infinity) [default=1]
  -l N           do not start new jobs if the load average is greater than N
  -n             dry run (don't run commands but act like they succeeded)
  -d MODE        enable debugging (use '-d list' to list modes)
  -t TOOL        run a subtool (use '-t list' to list subtools)
  -w FLAG        adjust warnings (use '-w list' to list warnings)
```

## 子工具

| 工具 | 说明 |
|------|------|
| `browse` | 在浏览器中浏览依赖图 |
| `clean` | 清理构建产物 |
| `cleandead` | 清理不再由 manifest 生成的旧文件 |
| `commands` | 列出重建目标所需的所有命令 |
| `compdb` | 导出 JSON 编译数据库 |
| `deps` | 显示 deps 日志中存储的依赖 |
| `graph` | 输出 Graphviz dot 文件 |
| `inputs` | 列出重建目标所需的所有输入 |
| `missingdeps` | 检查生成文件的 deps 日志依赖 |
| `query` | 显示路径的输入/输出 |
| `recompact` | 重新压缩内部数据结构 |
| `restat` | 更新构建日志中输出的 mtime |
| `rules` | 列出所有规则 |
| `targets` | 按规则或 DAG 深度列出目标 |
| `wincodepage` | 打印 Windows 代码页（仅 Windows） |

## 调试模式

```
-d stats       打印操作计数/耗时信息
-d explain     解释命令执行的原因
-d keepdepfile 保留 depfile 不被删除
-d keeprsp     保留 @response 文件
-d nostatcache 禁用 stat 缓存
```

## 项目结构

```
ninja-go/
  ninja/          Go 移植实现（入口：main.go）
  ninja-cpp/      原始 C++ 实现（参考）
  testdata/
    cmake-examples/   cmake 示例工程（用于编译测试）
  Makefile        构建和测试脚本
  test.bat        Windows 测试脚本
```

## cmake 示例测试

使用 cmake 生成 build.ninja，然后用 ninja-go 编译：

```bash
# 单个示例
make hello-cmake

# 按分组
make test-basic
make test-sub-projects

# 所有示例
make test-all

# Windows 命令行
test.bat hello-cmake
test.bat test-basic
```

前置依赖：Go 1.21+、CMake 3.5+、GCC/MinGW。

## 开发

```bash
go mod tidy        # 整理依赖
go test ./ninja/   # 运行测试
go build ./ninja/  # 构建
```

## 与 C++ 版本的差异

当前 Go 移植覆盖了核心构建功能：manifest 解析、依赖图、dirty 检测、并行构建、depfile/deps 日志、restat/generator 规则、pool、dyndep、响应文件等。

已知差异：
- `-t browse` 暂不支持（平台相关）
- 部分边界条件处理与 C++ 版本略有不同

## License

Apache 2.0（与原始 Ninja 一致）
