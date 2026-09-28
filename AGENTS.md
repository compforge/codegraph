# AGENTS.md

## 项目定位与边界

CodeGraph 是可内嵌的代码属性图 Go 库，从调用方提供的源码快照与材料中提取声明、解析关系，
提供携带来源位置、置信依据和局部覆盖信息的图查询能力。代码语义与关系证据由 CodeGraph 负责；
影响判定、评审组织、测试选择及执行策略由消费者负责。

实现组合 GoGraph 的属性图/Cypher、gotreesitter 的语法与事实提取、pond 的有界异步调度，
自身拥有代码身份、Namespace 组织、关系绑定、证据及快照发布契约；仓库读取、HTTP 服务和可视化归消费者。
根包提供语言无关的公共 API；Go、Python、JS/TS 有语言专有适配，其他注册 grammar 走通用声明提取。
语法可用性与关系解析能力分别报告，具体覆盖以 `Capabilities()` 与契约测试为准。

## 代码地图与核心模块

```text
VERSION                                # 项目版本
graph.go、node.go、relation.go、marker.go、error.go # 公共图模型、错误与能力声明
diagnostic.go                            # 局部信息缺口、关系类别与 outline 覆盖计数
access.go                                # 按源码路径、限定名和关系方向消费图事实
document.go、document_async.go            # Document 身份、后台构图、提前读取与 Wait 等待
build.go、build_graph.go                 # 范围构建、诊断及原子发布
build_extract.go                         # 有界并行提取、容量预留及确定性汇总
query.go                                # 只读查询及领域结果还原
internal/
  analysis/                             # 公共事实、实体引用、直接成员索引与阶段契约
  pipeline/                             # parser 生命周期与 Organize → Bind → Resolve 阶段屏障
  language/                             # 内置语言注册及能力声明
    adapters/                           # 阶段接口的具体实现
      golang/、python/、ecmascript/       # 语言专有提取、组织、绑定与关系规则
      generic/                          # 通用 grammar 兜底适配器
    syntax/、module/                     # 共享语法提取及模块绑定算法
  graphstore/                           # GoGraph、查询限制及引擎值转换
graph_test.go、example_test.go           # 契约测试与可执行示例
multilanguage_test.go、language_extension_test.go # 多语言隔离、能力边界与 grammar 扩展
tests/corpus/                            # 独立 Go 测试模块：固定仓库、Go 编译器 / Python AST 参照、覆盖测量与差异证据
docs/kernel.md                          # 稳定模型、主流程与设计依据
```

## 关键约定

1. 核心模型沿用 Graph、Node、Relation；Node.Kind 表达 Document、Struct、Interface、Field、Method、Function 等具体类别，直接映射为唯一节点标签。Symbol 与 Namespace 是逻辑概念，不新增上位标签或互斥三分类；Package、Module、语言自身的 Namespace 使用具体 Kind。
2. `declares` 从 Document 指向源码声明或组织贡献；`contains` 表达直接语义归属，允许 Namespace 嵌套。跨文件组织身份不依赖首个成员；单一位置缺失时沿 declares 读取证据。
3. spec、case、rule、link、doc 属于核心 marker 类型；关系的 confidence 描述证据强度，basis 记录建立关系的依据。路径证据聚合、距离衰减与业务阈值由消费者决定。
4. Document 是本库拥有的源码材料概念，既作为输入，也以同名节点类别入图；材料获取与选择由调用方负责。本库提供代码事实及节点、关系、路径和子图的通用查询；受影响文件的判定与排序、评审组织、测试选择及执行回退留在消费方。
5. AST 与图引擎内部类型不穿透公共 API；消费者通过 `Find`、`Node`、`RelationsFrom`、`RelationsTo` 或 Cypher 消费图事实；局部分析与未解析引用必须保留可辨识的覆盖信息。
   执行失败由 error 表达；候选关系使用 confidence 与 basis，信息缺口以局部诊断保留。局部缺口不否定无关事实，不自动触发消费者的全量回退。
6. 同一 Graph 只容纳同一源码快照；Document.ID 与对应 Document 节点 ID 相同。入队任务可提前返回单文件事实，后台构建跨文件关系并原子发布；Wait 只等待已提交工作完成。新增语言解析先声明能力并补契约测试；仅识别 grammar 不表示引用已解析，不得跨语言按同名猜测关系。
7. Core 拥有构建阶段、预算和原子发布；语言实现通过阶段接口返回事实、关系与诊断。语言专有证据由对应实现拥有，语法树只在 Extract 调用期间借用。直接成员统一由 contains 建索引，词法作用域不由 contains 推导。
8. 验证入口为 `make lint test build`，测试启用 race detector。
9. 根目录 `VERSION` 记录项目版本，格式为 `X.Y.Z`。任何代码文件变更（含测试代码、增删及重命名）必须在同一提交同步 bump `VERSION`，默认递增 patch；纯文档变更无需 bump。

## References

- [语言构建流程](docs/language-pipeline.md) — 阶段职责、语言接口与接入契约。

- [命名空间组织](docs/namespaces.md) — 逻辑概念、嵌套、源码贡献与语言绑定边界。
- [内核设计](docs/kernel.md) — 模型、构建与查询流程、依赖边界和验证状态。
- [真实仓库评测](docs/corpus.md) — 固定语料、独立参照、评分口径及验证入口；评测策略留在测试层。

- [Python 仓库评测](docs/python-corpus.md) — AST 事实清单、人工审定绑定及动态语义边界。
