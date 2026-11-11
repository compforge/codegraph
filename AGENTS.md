# AGENTS.md

## 项目定位与边界

CodeGraph 是解析和组织代码事实的 Go 库，首要目标是对输入 Documents 做静态分析，构造符号及其关系图。
Graph 是已接纳 Documents 范围内静态代码的结构化表示，可以是局部图。
解析事实先完整参与绑定；显式 AdmissionPolicy 再决定发布图的节点粒度，关系随端点保留。NodeKind 与 RelationKind
定义对象和联系的映射范围，源码结构与跨文件语义关系支持派生文件 outline、成员视图和关系路径。
Graph 消费侧以 Node + Relation 为唯一代码事实来源，outline、成员列表等结果均由图派生。
Outline 只在生产侧辅助提取声明关系；声明文档、位置和证据随节点或关系提供。
本库拥有代码事实与通用查询；repocli 据此确定改动和测试范围，CCR 据此确定 review 范围。
本库拥有可配置的标签机制与通用材料命名约定；消费者追加领域标签，并负责分析准入、仓库读取、影响判定、评审组织与执行策略。
使用入口见 [README](README.zh-CN.md)；本文件维护全局模型与开发约束，具体机制由下方 topic 文档维护。

## 核心模型与主流程

Document 提供快照材料；Extractor 有界解析并生成可跨快照复用、脱离 AST 的 Facts。
Builder 接纳 Facts 和调用方提供的 ResolutionContext；Graph 是一次成功构建的不可变读模型。
Symbol 与 Namespace 是节点的逻辑角色，分别表达声明与名称组织；Scope/Binding 独立表达可见性。
Relation 保存一次有向关系，Evidence 保存该关系的依据；材料缺失与分析能力限制通过局部诊断表达。

主流程：材料准入 → Extract → Organize → Bind → Resolve/lower → Select → Assemble/Publish。
全部实体登记后才绑定，全部语言完成 Bind 后才 Resolve；完整事实先参与分析，再按 AdmissionPolicy
选择公开实体与关系。Builder 校验端点及预算后原子发布，失败保留上一结果。
消费侧从 Node + Relation 派生 outline、成员和路径，BuildReport 只说明范围及覆盖条件。
生产侧可用 Facts 探索依赖，同一应用以 Graph 发布作为生产与消费的事实交接点。

根包组合生命周期与机制；公共值定义于 pkg，internal 不依赖根包。
internal/analysis 拥有事实和阶段契约，pipeline 编排语言适配器，graphstore 封装图引擎及查询预算。
AST 与引擎对象不穿透公共 API；材料获取、工程 Component、影响判断、评审及执行策略属于消费者。

## 代码地图与核心模块

```text
VERSION                                # 项目版本
pkg/                                   # 公共 Kind、图值类型、诊断、报告与证据精度
model.go                               # 根包兼容别名及装配辅助函数
document.go、document_node.go、facts.go # 输入材料、类型化 Document 节点与 Facts 检查视图
directory.go、tags.go                   # Directory 路径结构与开放 Tag 分类规则
extractor.go                           # 生产侧有界提取、任务与缓存
builder.go、graph.go                    # 接纳、构图发布及只读查询结果
compilation.go、build_limits.go          # 单轮构建协调、独立分析额度及失败原子性
selection.go、assembly.go                # 内部实体选择与按需公共图装配
resource_semantics.go、semantics/       # 通用资源引用机制与显式启用的平台语义包
namespaces.go、namespace_index.go       # 从图派生带证据的 namespace 祖先与共同归属
references.go                          # 将源码使用位置及其候选目标发布为图事实
session.go、builder_documents.go        # Builder 的异步材料提交与构建调度
internal/
  confidence/                           # pkg.Confidence 的内部兼容别名
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
docs/                                   # 按主题维护模型、构建、关系、材料与查询机制
```

## 关键约定

1. **上游已有能力直接复用**：gotreesitter 拥有解析、遍历和语法事实，GoGraph 拥有图存储及通用查询，pond 提供有界调度。先核对当前依赖输出；可扩展规则时优先补规则，上游已覆盖的实现必须删除。语法缺口先给最小复现，优先提交上游修复；临时适配写清缺口及退出条件。CodeGraph 负责语义绑定、证据、准入和发布，不建设平行解析器或图引擎。
2. **一种代码事实来源**：消费侧只读 Node + Relation，缺少信息时补图表达。Node 使用具体类别，declares、encloses、contains 与 Scope/Binding 分别表达源码贡献、声明嵌套、成员归属和可见性；Directory 路径层级不代替 Namespace。
3. **保留身份及不确定性**：源码使用、Import/Export 独立于目标是否已绑定而存在，不虚构外部目标。关系身份包含端点、种类和发生位置；独立 Evidence 取最高档，必要推导链取最低档，Confidence 不是概率。身份不依赖任务完成顺序或引擎内部编号。
4. **快照与生命周期分离**：Extractor 拥有解析和缓存，Builder 拥有材料、预算与原子发布，Graph 只读。补料重新组织和绑定，可以修订已有目标和证据，旧结果不变。语言适配器只消费显式材料和阶段索引，不获取文件或操作图存储。
5. **模型与策略分层**：NodeKind/RelationKind 位于 pkg，internal 可扩展中间 Kind，lowering 后只发布公共词表。Document/Directory 的物料分类使用开放式 Tags；直接格式与路径标签累加，祖先标签不复制。Admission 决定解析及图准入；清单语义属于格式适配器，工程边界属于消费者。
6. **领域效果显式接入**：库/平台 API 的资源语义由 semantics 包或调用方提供，语言适配器和核心不写平台 API 名称特判。uses_resource 表达文件关联，不推断读写或执行行为；语言原生入口由适配器识别，消费者决定分析与执行策略。
7. **失败、缺口与预算可区分**：执行失败通过 error 表达，预算失败不返回伪完整结果，局部诊断不否定无关事实。grammar 可用、声明可提取、引用可绑定是不同能力，新增语言能力同步更新注册声明及契约测试。消费者自行验证影响结论、回退与执行行为。
8. **验证与版本**：根包验证公共契约，internal 验证机制，tests/semantics 验证跨阶段语义，固定 corpus 用独立参照测量覆盖。入口为 `make lint test build`（含 race），`make eval` 单独评测；准备命令见 [评测入口](docs/corpus.md#执行与证据)。依赖及工具链版本以 go.mod 为准。代码文件变更（含测试、增删和重命名）在同一提交递增 `VERSION` 的 patch；纯文档变更无需 bump。

## References

- [资源引用](docs/resource-references.md) — 通用引用关系、显式 API 语义与平台适配边界。
- [使用指南](docs/usage.md) — 构建、补料、源码导航、Cypher 与诊断读取。
- [图模型与 lowering](docs/semantic-kinds.md) — 节点/关系词汇、映射标准及分析到公开模型的转换。
- [关系与证据](docs/relations.md) — 关系身份、Confidence、源码使用及 import/export 绑定链。
- [分析准入](docs/admission.md) — 材料分类、节点投影与解析事实边界。
- [Document 契约](docs/document.md) — 源码和 gitlink 材料、身份、接纳及图中表达。
- [命名空间组织](docs/namespaces.md) — 成员、嵌套、身份与源码贡献。
- [语言能力](docs/language-support.md) — 各语言的静态证据与限制。
- [语言构建流程](docs/language-pipeline.md) — 阶段职责、共享索引与适配器接入。
- [真实仓库评测](docs/corpus.md) — Go 独立参照、统一评分、工具准备及验证入口。
- [Python 仓库评测](docs/python-corpus.md)、[TypeScript 仓库评测](docs/typescript-corpus.md) — 各语言参照及动态语义边界。
