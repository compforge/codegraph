# AGENTS.md

## 项目定位与边界

CodeGraph 是解析和组织代码事实的 Go 库，首要目标是对输入 Documents 做静态分析，构造符号及其关系图。
Graph 是已接纳 Documents 范围内静态代码的结构化表示，可以是局部图。NodeKind 与 RelationKind
定义对象和联系的映射范围，源码结构与跨文件语义关系支持派生文件 outline、成员视图和关系路径。
Graph 消费侧以 Node + Relation 为唯一代码事实来源，outline、成员列表等结果均由图派生。
Outline 只在生产侧辅助提取声明关系；声明文档、位置和证据随节点或关系提供。
本库拥有代码事实与通用查询；repocli 据此确定改动和测试范围，CCR 据此确定 review 范围。
仓库读取、影响判定、评审组织及执行策略由消费者负责。
稳定模型与设计理由见 [内核设计](docs/kernel.md)，使用入口见 [README](README.zh-CN.md)。

## 代码地图与核心模块

```text
VERSION                                # 项目版本
model.go                               # 公共图值类型、诊断、报告与错误
document.go、document_node.go、facts.go # 输入材料、类型化 Document 节点与 Facts 检查视图
extractor.go                           # 生产侧有界提取、任务与缓存
builder.go、graph.go                    # 接纳、构图发布及只读查询结果
namespaces.go、namespace_index.go       # 从图派生带证据的 namespace 祖先与共同归属
references.go                          # 将源码使用位置及其候选目标发布为图事实
session.go、builder_documents.go        # Builder 的异步材料提交与构建调度
internal/
  confidence/                           # 公共 API 与分析过程共用的证据精度及排序
  analysis/                             # 实体、Namespace、Scope/Binding、Relation/Evidence 登记及阶段契约
  pipeline/                             # parser 生命周期与 Organize → Bind → Resolve 阶段屏障
  language/                             # 内置语言注册及能力声明
    adapters/                           # 阶段接口的具体实现
      golang/、python/、ecmascript/       # 语言专有提取、组织、绑定与关系规则
      manifest/                          # 清单格式识别、元数据提取与模块声明
      generic/                          # 通用 grammar 兜底适配器
    syntax/、module/                     # 共享语法提取及模块绑定算法
  graphstore/                           # GoGraph 引擎、查询限制及引擎值转换
*_test.go                              # 按核心对象组织的机制测试、公共契约与可执行示例
tests/semantics/                         # 通过公共 API 验证语言绑定、关系与多阶段语义回归
tests/corpus/                            # 独立 Go 测试模块：固定仓库、Go / Python / TypeScript 独立参照、覆盖测量与差异证据
docs/kernel.md                          # 稳定模型、主流程与设计依据
```

## 关键约定

1. Node 使用具体语言类别；Symbol 与 Namespace 是可重叠的逻辑角色。源码贡献、声明的词法嵌套、语义成员归属和词法可见性分别由 declares、encloses、contains 与 Scope/Binding 表达。Document 的语言组织根通过 in_namespace 发布；路径语义由语言适配器确定，公共层不预设组织层级。
2. Reference 以 referenceKind 区分源码使用；Import / Export 保留源码绑定项，references 连接直接目标，aliases 保留名称链，decorates 从修饰应用指向被修饰声明。它们独立于目标绑定存在；局部图保留候选与缺口，不虚构外部目标。关系身份保留端点、种类及发生位置；独立 Evidence 取最高档，单条证据的必要推导链取最低档，不将证据强度解释为业务影响概率。源码使用与绑定模型见内核设计。
3. 同一 Graph 只容纳同一快照。Extractor 拥有解析与缓存；Builder 拥有构图、预算及原子发布，Graph 是只读结果；补入 Document 后重新组织与绑定，最新结果可修订既有节点、关系和证据，旧结果不变。语言实现通过阶段接口提供事实、关系与诊断，不获取材料或直接操作图存储。
4. DocumentKind 表达材料类别，grammar 表达语法格式；清单语义由格式适配器提取，工程 Component 由消费者判定。消费侧视图只读取 Node + Relation；缺少信息时补足图表达，不回读 Facts 或解析树。Facts 服务生产侧的依赖探索、解析与构图；Outline 在提取内部复用 gotreesitter，转换为声明和覆盖诊断后释放。AST 与引擎对象不穿透公共 API。新增语言能力同步更新注册声明及契约测试，grammar 可用不等于语义完整。
5. 执行失败由 error 表达；局部诊断不否定无关事实。根包测试验证公共契约，内部机制测试随所属包维护；跨阶段语言回归放在 tests/semantics，真实语料验证语义覆盖，消费者验证自身策略与执行行为。
6. 验证入口为 `make lint test build`，测试启用 race detector；`make eval` 按语言评测符号、关系与 namespace 组织结构；工具准备和语料命令见 [评测执行入口](docs/corpus.md#执行与证据)。
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
