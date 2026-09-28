# AGENTS.md

## 项目定位与边界

CodeGraph 是可内嵌的代码属性图 Go 库，将调用方提供的快照材料转为携带来源、证据和局部诊断的图。
本库拥有代码事实与通用查询；仓库读取、影响判定、评审组织及执行策略由消费者负责。
稳定模型与设计理由见 [内核设计](docs/kernel.md)，使用入口见 [README](README.zh-CN.md)。

## 代码地图与核心模块

```text
VERSION                                # 项目版本
graph.go、node.go、relation.go、marker.go、error.go # 公共图模型、错误与能力声明
diagnostic.go                            # 局部信息缺口、关系类别与 outline 覆盖计数
access.go                                # 按源码路径、限定名和关系方向消费图事实
document.go、document_async.go            # Document 身份、后台构图、提前读取与 Wait 等待
build.go、build_graph.go                 # 范围构建、诊断及原子发布
build_extract.go、material.go            # 有界提取、容量预留、统一材料分类及确定性汇总
query.go                                # 只读查询及领域结果还原
internal/
  analysis/                             # 实体、Namespace、Scope/Binding、Relation/Evidence 登记及阶段契约
  pipeline/                             # parser 生命周期与 Organize → Bind → Resolve 阶段屏障
  language/                             # 内置语言注册及能力声明
    adapters/                           # 阶段接口的具体实现
      golang/、python/、ecmascript/       # 语言专有提取、组织、绑定与关系规则
      generic/                          # 通用 grammar 兜底适配器
    syntax/、module/                     # 共享语法提取及模块绑定算法
  graphstore/                           # GoGraph、查询限制及引擎值转换
graph_test.go、example_test.go           # 契约测试与可执行示例
multilanguage_test.go、language_extension_test.go # 多语言隔离、能力边界与 grammar 扩展
tests/corpus/                            # 独立 Go 测试模块：固定仓库、Go / Python / TypeScript 独立参照、覆盖测量与差异证据
docs/kernel.md                          # 稳定模型、主流程与设计依据
```

## 关键约定

1. Node 使用具体语言类别；Symbol 与 Namespace 是可重叠的逻辑角色。源码贡献、成员归属和词法可见性分别由 declares、contains 与 Scope/Binding 表达。
2. 关系身份保留端点、种类及发生位置，多条依据汇入 Evidence。保留候选与局部缺口，不虚构目标，也不将证据强度解释为业务影响概率。
3. 同一 Graph 只容纳同一快照。Core 拥有调度、预算及原子发布；语言实现通过阶段接口提供事实、关系与诊断，不获取材料或直接操作图存储。
4. AST 与引擎对象不穿透公共 API，提取结果脱离 parser 生命周期。新增语言能力同步更新注册声明及契约测试，grammar 可用不等于语义完整。
5. 执行失败由 error 表达；局部诊断不否定无关事实。库测试验证图契约，真实语料验证语义覆盖，消费者验证自身策略与执行行为。
6. 验证入口为 `make lint test build`，测试启用 race detector；工具准备和语料命令见 [评测执行入口](docs/corpus.md#执行与证据)。
7. `VERSION` 格式为 X.Y.Z，任何代码文件变更（含测试、增删及重命名）须在同一提交 bump，默认递增 patch；纯文档变更无需 bump。

## References

- [使用指南](docs/usage.md) — 构建、补料、源码导航、Cypher 与诊断读取。
- [内核设计](docs/kernel.md) — 核心模型、主流程与责任边界。
- [Document 契约](docs/document.md) — 源码和 gitlink 材料、身份、接纳及图中表达。
- [命名空间组织](docs/namespaces.md) — 成员、嵌套、身份与源码贡献。
- [语言能力](docs/language-support.md) — 各语言的静态证据与限制。
- [语言构建流程](docs/language-pipeline.md) — 阶段职责、共享索引与适配器接入。
- [真实仓库评测](docs/corpus.md) — Go 独立参照、统一评分、工具准备及验证入口。
- [Python 仓库评测](docs/python-corpus.md)、[TypeScript 仓库评测](docs/typescript-corpus.md) — 各语言参照及动态语义边界。
