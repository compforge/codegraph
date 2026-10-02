# AGENTS.md

## 项目定位与边界

CodeGraph 是可内嵌的代码属性图 Go 库，将调用方提供的快照材料转为携带来源、证据和局部诊断的图。
本库拥有代码事实与通用查询；仓库读取、影响判定、评审组织及执行策略由消费者负责。
稳定模型与设计理由见 [内核设计](docs/kernel.md)，使用入口见 [README](README.zh-CN.md)。

## 代码地图与核心模块

```text
VERSION                                # 项目版本
model.go                               # 公共图值类型、诊断、报告与错误
document.go、facts.go                   # 输入材料、身份与完整 Facts 检查视图
extractor.go、outline.go                # 有界提取、任务、缓存与上游 Outline 访问
builder.go、graph.go                    # 接纳、构图发布及只读查询结果
session.go、compat.go                   # 私有异步会话与兼容入口
internal/
  confidence/                           # 公共 API 与分析过程共用的证据精度及排序
  analysis/                             # 实体、Namespace、Scope/Binding、Relation/Evidence 登记及阶段契约
  pipeline/                             # parser 生命周期与 Organize → Bind → Resolve 阶段屏障
  language/                             # 内置语言注册及能力声明
    adapters/                           # 阶段接口的具体实现
      golang/、python/、ecmascript/       # 语言专有提取、组织、绑定与关系规则
      generic/                          # 通用 grammar 兜底适配器
    syntax/、module/                     # 共享语法提取及模块绑定算法
  graphstore/                           # GoGraph 引擎、查询限制及引擎值转换
*_test.go                              # 按核心对象组织的机制测试、公共契约与可执行示例
tests/semantics/                         # 通过公共 API 验证语言绑定、关系与多阶段语义回归
tests/corpus/                            # 独立 Go 测试模块：固定仓库、Go / Python / TypeScript 独立参照、覆盖测量与差异证据
docs/kernel.md                          # 稳定模型、主流程与设计依据
```

## 关键约定

1. Node 使用具体语言类别；Symbol 与 Namespace 是可重叠的逻辑角色。源码贡献、成员归属和词法可见性分别由 declares、contains 与 Scope/Binding 表达。
2. 关系身份保留端点、种类及发生位置，多条依据汇入 Evidence，Relation.Confidence 由 Evidence 的最高档推导；单条证据的必要推导链取最低档。保留候选与局部缺口，不虚构目标，也不将证据强度解释为业务影响概率。
3. 同一 Graph 只容纳同一快照。Extractor 拥有解析与缓存；Builder 拥有构图、预算及原子发布，Graph 是只读结果；语言实现通过阶段接口提供事实、关系与诊断，不获取材料或直接操作图存储。
4. AST 与引擎对象不穿透公共 API，提取结果脱离 parser 生命周期；Outline 直接复用 gotreesitter 的 OutlineSymbol / OutlineReport 值类型。新增语言能力同步更新注册声明及契约测试，grammar 可用不等于语义完整。
5. 执行失败由 error 表达；局部诊断不否定无关事实。根包测试验证公共契约，内部机制测试随所属包维护；跨阶段语言回归放在 tests/semantics，真实语料验证语义覆盖，消费者验证自身策略与执行行为。
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
