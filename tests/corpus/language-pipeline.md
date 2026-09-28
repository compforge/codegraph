# 语言构建流程重构验证

## 比较边界

基线为 `d3fb7476033dc751c2750f8e1610c24a9a6b2477`（0.7.10），重构版本为 0.7.11。
两轮使用相同固定仓库、输入文件、oracle、工具链、profile 和 evaluator。
这次重构没有修改 corpus evaluator；通过 `CORPUS_BASELINE_DIR` 执行同口径回归门禁。

- 当前源码 SHA256：`215b579508a1809356ef7bcc053f3f304664f0494af432f40865fd34e91f613a`
- Evaluator SHA256：`e94df34532d84d286f021f0eeb93d7259d3874f33893597342229c24467e4d1a`

## 结果

`make fix lint test build test-corpus` 通过，包括 root/corpus race 测试、Python oracle 单元测试，
以及阶段顺序、自定义语言、统一直接成员查询和错误/取消/预算返回契约。

以下五仓 `graph.json` 解析后的内容逐项相等，覆盖节点、关系端点、身份、置信依据、诊断和提取事实；
不是仅比较计数。各评测项的 measurements 与 bindings 也相等。

| 固定仓库 | 图与事实 | 当前 graph.json SHA256 |
|---|---|---|
| go-stdx | 相等 | `93e7518fc4e8d3d6f5c61cdc6ea18e9c4525891292d5e7a2f29c15d191c6f2dd` |
| agentgo | 相等 | `18051479e4fb0b94cce77dea6c3918f119dac8c17d4de51e2cd628eed8056c1b` |
| repocli | 相等 | `4f510809f7b4c7090fb7c2a3f7b6c776ab38d0090eccc2c557d821324fa8bb51` |
| python-stdx | 相等 | `17b7e11c4ac173dc21e4fef766b4a37c59ea250f86d5aa97d39197dd9e50beed` |
| agentue | 相等 | `acc5fd2727e89eb811fce55ff5b7d911a194fbdacc1b67121115c86bcc87fc74` |

另外以相同源码对基线与当前实现执行了三个混合 JS/TS 快照比较：

| 场景 | 节点 | 关系 | 完整节点、关系、报告、Extract 事实 |
|---|---:|---:|---|
| 跨 JS/TS 的类继承与调用 | 10 | 22 | 相等 |
| 再导出链与参数遮蔽 | 7 | 10 | 相等 |
| 显式嵌套 namespace 与模块候选 | 7 | 13 | 相等 |

这些比较证明已验证材料上的行为保持，不表示新增语言覆盖、静态解析完备性或性能提升。
真实语料中的组织语义仍沿用现有测量边界，不宣称获得了独立的组织 ground truth。

## 复验方式

先在基线 checkout 运行 `make test-corpus CORPUS_REPORT_DIR=<baseline>`，再在待验 checkout 运行
`make test-corpus CORPUS_REPORT_DIR=<current> CORPUS_BASELINE_DIR=<baseline>`。
两次运行的 evaluator、工具链和输入必须相同；分别比较每个仓库的 graph.json 与 report.json 中评测项。
原始报告由测试写入指定目录，不纳入源码仓库。
