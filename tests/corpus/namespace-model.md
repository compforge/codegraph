# Namespace 统一实体模型验证

## 比较边界

基线为 `cd8ee2b03496460c4162ab6150809e2d771bedd3`（0.7.12），当前版本为 0.7.13。
固定仓库、输入、oracle、profile、工具链和 evaluator 相同，执行同口径 baseline 门禁。

- 当前源码 SHA256：`dca44e951130bbfd6c9ee6853e443163e59460efc7cd43cc3286749f1a2602e9`
- Evaluator SHA256：`e94df34532d84d286f021f0eeb93d7259d3874f33893597342229c24467e4d1a`

## 结果

`make fix lint test build test-corpus` 通过，包含 root/corpus race 测试与 Python oracle 单元测试。

组织实体 ID 从 `namespace:<hash>` 统一为 `node:<hash>`，相连关系的 ID 随端点变化。
比较时仅对基线执行此确定性 ID 映射，并按端点、关系类别和源码位置重算关系 ID，随后按 ID 排序。
其余字段均参与比较，没有删除位置、confidence、basis 或诊断。

| 固定仓库 | 映射的组织 ID 数 | 节点 | 关系 | 完整图、facts、report 与 evaluation |
|---|---:|---:|---:|---|
| go-stdx | 11 | 91 | 288 | 相等 |
| agentgo | 13 | 2668 | 16314 | 相等 |
| repocli | 9 | 685 | 4581 | 相等 |
| python-stdx | 43 | 508 | 1806 | 相等 |
| agentue | 11 | 126 | 606 | 相等 |

## 结构与 ECMAScript 契约

- 声明实体可直接作为 Document 的语义根，Namespace 视图不产生重复实体或 contains 自环。
- 先登记各语言的全部实体，再按名称索引成员，跨语言阶段的前向组织引用可被查到。
- Package、Module、显式 Namespace、Class 与跨文件接收者方法复用成员视图；源码贡献独立读取。
- JS/TS/TSX 文件提供 Module；模块导入和模块名称引用指向 Module，类与方法保留嵌套成员关系。
- 同名不同路径模块不合并，空模块有独立身份，增量批次与一次构建的完整图相等。
- 新组织节点和关系计入预算；超限保持上次已发布图。

这些场景分别由 `internal/pipeline/build_test.go`、`namespace_ecmascript_test.go` 及现有语言契约覆盖。
五仓语料只覆盖 Go/Python，ECMAScript 的变化由明确源码场景验证；不宣称获得了 JS/TS 真实仓库 oracle，
也不证明运行时加载模式、动态 namespace package 或静态解析完备性。

## 复验

基线与待验 checkout 分别执行 `make test-corpus CORPUS_REPORT_DIR=<report-dir>`；待验增加
`CORPUS_BASELINE_DIR=<baseline-report-dir>`。完整图比较按上述确定性 ID 映射进行。
原始图与报告保存在测试指定目录，不纳入源码仓库。
