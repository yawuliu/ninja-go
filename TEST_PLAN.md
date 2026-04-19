# Ninja-Go 测试计划

## 当前状态分析

### 已有测试
- `builder_test.go` - Builder 基础测试（17个测试用例）
- `depfile_parser_test.go` - Depfile 解析测试（25个测试用例）
- `graph_test.go` - 图结构测试（框架，部分实现）
- `buildlog_test.go` - 构建日志测试
- `depslog_test.go` - 依赖日志测试
- `builder_dyndep_test.go` - 动态依赖测试

### 测试覆盖率缺口

#### 1. Lexer 测试 (lexer_test.go) - 缺失
需要测试：
- Token 识别：BUILD, RULE, POOL, DEFAULT, INCLUDE, SUBNINJA 等关键字
- 特殊符号：COLON, PIPE, PIPE2, PIPEAT, EQUALS
- 字符串解析：ReadIdent, ReadPath, ReadVarValue
- 变量展开：$var, ${var}
- 转义序列：$$, $:, $ , $\n
#### 2. Parser 测试 (parser_test.go) - 缺失
需要测试：
- Rule 定义解析
- Build 语句解析（含 implicit inputs, order-only inputs, validations）
- Pool 定义解析
- Default 语句解析
- Include/Subninja 解析
- 变量绑定和作用域

#### 3. EvalString 测试 - 缺失
需要测试：
- AddText, AddSpecial
- Evaluate（变量展开）
- Unparse（序列化）
- 边界情况（空字符串、单token、多片段）

#### 4. State/Node/Edge 测试 - 部分缺失
需要测试：
- Node 状态管理（Stat, ResetState）
- Edge 输入输出管理
- State 的节点查找和添加
- 循环依赖检测

#### 5. Plan 测试 - 缺失
需要测试：
- AddTarget
- FindWork/EdgeFinished
- scheduleWork
- computeCriticalPath
- CleanNode
- DyndepsLoaded

#### 6. Pool 测试 - 缺失
需要测试：
- 深度限制
- Edge 调度
- Delay/Retrieve 机制

#### 7. Queue 测试 - 缺失
需要测试：
- EdgePriorityQueue 的 Push/Pop
- 优先级排序（CriticalPathWeight）

#### 8. Scanner 测试 - 缺失
需要测试：
- RecomputeDirty
- 文件存在性检查
- 时间戳比较

#### 9. ManifestParser 测试 - 缺失
需要测试：
- 完整 manifest 文件解析
- 错误处理
- 版本检查

## 测试优先级

### P0 - 核心功能
1. Lexer 测试 - 词法分析是基础
2. Parser 测试 - 语法解析是关键
3. EvalString 测试 - 变量展开是核心功能
4. Plan 测试 - 构建计划是核心算法

### P1 - 重要功能
5. State/Node/Edge 测试 - 数据结构
6. Scanner 测试 - 脏检查逻辑
7. Pool/Queue 测试 - 并发控制

### P2 - 完整性和集成
8. ManifestParser 测试
9. 集成测试
10. 边界情况和错误处理

## 实施计划

### 阶段1: 基础单元测试
- [ ] lexer_test.go - 完整的词法分析测试
- [ ] eval_test.go - EvalString 测试
- [ ] parser_test.go - 基础解析测试

### 阶段2: 核心逻辑测试
- [ ] plan_test.go - 构建计划测试
- [ ] state_test.go - 状态管理测试
- [ ] scanner_test.go - 依赖扫描测试

### 阶段3: 辅助组件测试
- [ ] pool_test.go - 资源池测试
- [ ] queue_test.go - 优先队列测试
- [ ] manifest_parser_test.go - 完整 manifest 测试

### 阶段4: 集成和修复
- [ ] integration_test.go - 端到端测试
- [ ] 运行所有测试
- [ ] 修复发现的 bug
- [ ] 完善测试覆盖率

## 测试设计原则

1. **单元测试**：每个函数/方法至少一个测试用例
2. **边界测试**：空输入、大输入、特殊字符
3. **错误测试**：错误处理路径
4. **集成测试**：模拟完整构建场景
5. **Mock 使用**：文件系统、命令执行使用 Mock
