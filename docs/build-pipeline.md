# 构图流程

## 模型与职责

CodeGraph 从同一快照的 Document 提取事实，由语言规则建立组织与绑定，再发布带源码证据的图。
| 能力 | 职责 |
| --- | --- |
| gotreesitter | 识别 grammar、解析和遍历语法树，提供声明、声明头范围、调用与参数等单文件语法事实。 |
| CodeGraph | 把单文件事实与本轮材料、模块上下文结合，绑定目标、登记证据，再按准入策略选择并发布节点与关系。 |
| GoGraph | 存储供查询的属性图并执行 Cypher。CodeGraph 的 graphstore 转换图值，并落实只读查询和预算契约。 |

全局职责与发布不变量见 [开发约定](../AGENTS.md)，语言覆盖见 [语言能力](language-support.md)。

公共分析层以 Entity 统一登记 Document、源码声明及合成实体，并使用同一条物化路径。
Ref 表达身份来源，与实体是否组织成员无关。Namespace 是同一 Entity 的成员组织视图，
Package/Module、显式 Namespace 和 Class 通过相同视图查成员与源码贡献；跨文件接收者归属
在绑定后补入。继承查找在直接成员之上遍历已绑定的 extends。
词法作用域、名称遮蔽和导出规则由语言实现解释，不由 contains 自动推导。

## 提取、缓存与快照发布

Document 提供逻辑路径及内容或 gitlink；材料身份、标签与清单契约见 [Document](document.md)。
Extractor 负责有界解析并产出脱离 AST 的 Facts，包括词法线索、语言证据、候选关系与局部诊断。
解析的复用单位是 Facts，关系绑定的单位是 Builder 当前接纳的材料集合；Facts 的公开检查视图
服务生产侧依赖探索，修改该视图不会改变内部构图材料。

Extractor 限制并发、单文件字节和解析时间；共享 Extractor 可统一多个快照的解析容量。
调用方持有有界 ExtractionCache，缓存身份包含路径、材料类型及内容，容量不足时照常提取。
显式传递 Facts 不依赖缓存命中；缓存契约见 [源码材料](document.md#源码材料)。

Builder 接纳 Facts 和快照专属 ResolutionContext；后者承载调用方已获得的模块根与导入路径证据，
其精度限制后续绑定。新增、删除或重命名依赖，以及模块上下文变化，都需要对本轮全部材料重新组织与绑定。
语言适配器解释显式材料中的清单；文件获取、工程边界与探索范围由调用方负责。

构建成功后才替换 Result；取消和执行失败保留上一次成功结果，生产侧可用 AddFailure 记录单文件解析缺口。
补料可以修订已有归属、候选目标及证据，已返回的 Graph 保持不变。Graph 只提供查询，
不持有生产会话或返回解析材料，Cypher 索引在首次查询时建立。
异步补料由 Builder 按需创建的私有 session 协调；Wait 只等待既有工作，取消等待不会取消构建。
包级 Build 与 Builder.Build 返回相同的不可变结果；精确 API 契约见 [使用指南](usage.md)。

## 阶段与数据流

| 阶段 | Core 责任 | 语言接口与产物 |
|---|---|---|
| Extract | 公共 Extractor 管理并发、缓存和解析期限 | `language.Extract` 借用语法树调用适配器，返回脱离 AST 的单文件 Facts |
| Organize | 登记全部实体后统一建立成员索引 | Organizer 返回实体、Document 的语义根及关系证据 |
| Bind | 汇总绑定结果，补齐成员和类型关系 | Binder 返回导入、类型、接收者关系及本轮 Resolver |
| Resolve | 汇总关系证据和局部缺口 | RelationResolver 使用完成绑定的索引解析引用与调用 |
| Select | entitySelector 按准入策略选择内部实体与 bridges | 由 Core 执行 |
| Assemble / Publish | graphAssembler 按需生成公共值并检查发布预算，Builder 原子发布 | 由 Core 执行 |

`pipeline.Analyzer` 协调本轮材料的语义分析。所有语言完成 Organize 后才开始 Bind，所有 Bind 完成后才开始 Resolve。
这使晚加入的基类、接收者类型以及混合 JS/TS 文件能够参与同一轮关系解析。
语言内部可进行有界类型传播；它不能提前发布结果或自行启动另一轮构建。

语言实现只消费调用方提供的源码材料和当前批次索引，不读取仓库、安装依赖或操作图存储。
Binder 与 Resolver 将输入索引视为只读，其关系结果由 Core 登记；失败或取消返回错误，整个批次不发布。
候选目标保留 confidence 与 basis，已识别的遮蔽阻止同名回退，未解析引用保留局部诊断。

### 一次调用如何流经各层

以 `function handle() { const local = run(); }` 为例，Facts 保留 handle、local、run 的使用位置与作用域；
绑定确定 run 的目标，语义层保留已有的 `handle ─encloses→ local ─calls→ run` 及证据。
分析策略若判定 local 连接两个核心实体，就把它选为 bridge，再生成三个节点和两条原始关系。
若完整策略还要求使用位置，则另外从调用事实生成 CallSite；分析策略无需先造这个节点再删除。

这里不会因希望图更紧凑就把路径改写为 `handle calls run`。声明归属与调用的组合并不自动证明
一条新的直接调用；语义派生需要语言规则及证据，与对象生成是不同职责。

选择理由与预算见[材料准入与发布范围](admission.md)。

## 共享索引

`BuildScope` 是本轮材料集合；`Scope` 是源码可见范围，`Binding` 是该范围的名称绑定。
`BindResult` 返回关系贡献、诊断及本轮 Resolver。
Python/ECMAScript 的 dialect 在单文件提取中登记脱离 AST 的 SymbolTable，后续引用、调用和
接收者类型线索共享其查找结果；语言实现负责声明位置、父级跳转、参数和块等规则。
普通函数调用复用标识符引用的绑定结果；未提取到词法父级不证明顶层可见。
未知局部绑定仍需阻止同名回退，未证明的动态行为以局部缺口表达。

显式资源语义先从通用调用参数事实产生本轮路径引用；所有语言关系解析后，Analyzer 将其绑定到
已接纳的 Document，再将 Index 交给选择/发布阶段。规则不进入提取缓存，也不修改已发布的 Index。

所有阶段贡献进入 `Index.Add`：按 Source/Target/Kind/源码位置登记 occurrence，合并、去重、排序
Evidence，并派生 confidence。Organizer 的 document 根发布为 in_namespace，根可为源码声明
或合成实体；路径的组织语义留在语言适配器。Namespace 的成员与源码贡献由同一登记结果投影。
不同 Kind 或源码位置不能去重；来源不同的证据不能最后写入覆盖。语言阶段提供单条贡献时可用
内部 Edge 的 Basis/Confidence 简写，聚合后以 Evidence 为准。

## 语言接入

`internal/analysis/stages.go` 定义阶段接口，`internal/language` 统一注册实现与能力说明。
具体阶段实现集中在 `language/adapters/`；适配器复用的语法与绑定算法位于 `language/syntax/`、
`language/module/`，共享组件依赖分析契约，由各适配器提供语言策略。
新增语言选择需要实现的阶段，并把实际能力和限制放入同一个注册项；Core 的阶段编排不识别具体语言名。
grammar 识别、清单文件名和扩展名别名由 gotreesitter 注册表统一决定；CodeGraph 注册表只选择语义适配器。
仅注册 grammar 的语言继续使用通用 outline，报告关系解析覆盖缺口。

Go、Python、ECMAScript 各自拥有语义规则；共享算法由它们显式调用：

- gotreesitter 的 FactProgram 提供声明、结构化签名与调用参数。JS/TS 的构造调用来自 `FactConstructorCalls`，适配器结合词法作用域、导入项和接收者线索绑定目标。
- `grammars.HeaderRange` 提供声明头的字节范围。CodeGraph 将范围关联到声明，供 Node.Signature 与 SignatureLocation 使用。同一个语法节点可以对应多个声明，例如 Go 的 `var a, b int`。
- 通过 FactProgram 补充规则登记局部声明和 namespace 内的绑定。通用 outline 使用上游 Outliner，命名节点遍历复用上游 Walk。
- Python 和 JS/TS 共享模块绑定、转导出及候选汇总算法。导入项及别名位置来自 `grammars.ImportItems`，继承表达式及范围来自 `FactHeritage`。适配器结合词法作用域、导入路径和导出规则绑定跨文件目标，动态基类保留诊断。
- 文档、marker 与声明的归属由语言适配器确定。Go 入口识别复用已有语法树，Go 类型传播所需的证据由 Extension 携带。

解析直接使用 LangEntry.ParseStrict，由上游处理 token source 和 cooperative cancellation；
CodeGraph 提供仅覆盖解析阶段的 context 预算，释放返回树，并按事实接纳要求拒绝不完整语法树。

注册项不保存跨快照可变状态。Extract 只能在调用期间借用语法树，返回的事实及语言扩展证据必须
脱离 parser 且只读；Binder 返回的 Resolver 状态属于本轮构建。公共索引始终由当前源码快照重建。

语法层按 grammar 实例、语言规则和提取配置复用只读的 FactProgram 与 Outliner，避免逐文件重复编译。
编译结果随进程复用，不持有源码、AST 或提取结果；并发提取各自拥有结果缓冲。

## 验证边界

接入需要验证阶段依赖、源码身份、语言隔离、取消与预算，并为实际新增的语义建立契约。
测试适配器验证无需修改 Core 即可接入组织、绑定和关系解析；现有真实语言契约验证 grammar 兜底及语义规则。
结构调整应比较固定语料的完整图、诊断和独立评测结果，而不只比较节点数量。

这些接口是内部实现边界，公共 API 仍为 Graph、Document、Facts、Node、Relation 和查询能力。
影响传播、排序、测试选择及执行策略属于消费者。
