# CodeGraph 内核设计

## 定位与责任

CodeGraph 负责解析和组织代码事实，首要目标是对调用方提供的 Documents 做静态分析，构造符号及其关系图。Graph 是静态代码的结构化组织与表示，以节点、关系及其属性支持结构化存储和查询，同时保存源码结构与语义关系。
分析经过解析、声明提取、组织、绑定与关系解析，原子发布携带来源位置、关系证据和局部覆盖信息的 Graph。
Graph 消费侧以 Node + Relation 为唯一代码事实来源，文件 outline、类型成员与调用路径均从图派生。
声明文档、源码位置与关系证据作为节点或关系的属性提供。
图同时组织文件结构（Document、Directory）与程序结构（语言组织实体、声明及其关系）。
CodeGraph 提供可配置的标签机制；调用方负责材料获取、分类策略和标签的业务含义，并将图事实解释为评审、影响分析或执行决策。

| 层次 | 责任 |
|---|---|
| CodeGraph | 材料与路径结构、代码身份、声明与关系、来源证据、标签机制、局部诊断、通用图查询 |
| repocli | 识别改动入口，结合代码事实确定改动影响与测试范围 |
| CCR | 结合改动与代码事实确定 review 范围，组织评审单元、上下文和预算 |
| 执行消费者（如 devloop） | 按项目契约选择并执行检查，处理分析失败时的回退 |

不同消费者可以选择不同范围与遍历方向，共用可追溯的原始事实。

本文围绕两个核心问题展开：CodeGraph 的责任边界，以及 NodeKind / RelationKind 是否足以准确表达
输入代码的对象与联系。生产流程负责兑现这份模型，消费者据此形成自己的分析结果。

## 核心模型

### 静态代码与图的对应

Graph 是输入代码在给定快照和分析范围内的静态表示。可以用下面的对应理解它保留的对象与联系：

| 静态代码 | Graph 中的表达 |
|---|---|
| Repository | Graph；可以只覆盖仓库的一部分材料 |
| 物理目录 | Directory；由已接纳材料的路径补齐祖先层级 |
| 语言组织结构 | Namespace（逻辑角色）；按语言规则表现为 Package、Module 等节点 |
| File 或 Git 材料条目 | Document；普通源码、go.mod 等文件以及 gitlink 都有材料身份，按材料种类与分析能力处理 |
| Class、Interface、Field、Function 等声明 | Symbol（逻辑角色）；节点保留具体语言类别 |
| 源码中的调用表达式与标识符使用 | Reference；referenceKind 区分调用、引用、基类、接口及修饰使用，目标不在当前图中也有源码身份 |
| 导入、导出及转导出项 | Import / Export；保留每项名称、别名、模块说明符与位置 |
| 源码中的包含、归属、调用、引用等联系 | Relation；不同关系保留各自的语义和来源依据 |
| 声明、名称或关系发生的文本位置 | Node / Relation 上的源码位置属性 |

目录与 Namespace 不固定一一对应：一个包可以由多个文件共同组成，一个文件也可以声明嵌套的
逻辑组织。Document 提供材料边界，Symbol 提供声明身份，Relation 把源码组织与语义联系接起来；
章节式的大纲是这些事实的一种读取方式。

### NodeKind 与 RelationKind 的覆盖与合理性

NodeKind 与 RelationKind 是代码到图的映射词汇，定义模型能保留哪些事实。评估内核时，既要检查
职责是否清楚，也要检查这些分类是否全面、语义是否合理：只靠现有节点与关系，消费者能否还原
所需的程序结构，能否区分含义不同的联系。消费需求暴露的信息缺口应回到这一层审视。

当前节点按所表达的代码对象分组如下；分组用于解释语义，不增加图标签或新的节点身份。
完整枚举的代码定义见 [model.go](../model.go)，各语言实际提取范围见 [语言能力](language-support.md)。

| 对象 | 当前 NodeKind | 分类依据 |
|---|---|---|
| 输入材料 | `Document` | 已接纳材料的身份；DocumentKind 描述 source、gitlink 或 unknown；Tags 表达 manifest 等可叠加分类，独立于 grammar 与解析是否成功 |
| 路径结构 | `Directory` | 已提供材料的祖先目录，按路径去重，Tags 直接匹配自身路径 |
| 名称与成员组织 | `Package`、`Module`、`Namespace` | 语言中的组织实体，不将物理目录直接等同于命名空间 |
| 类型声明 | `Struct`、`Interface`、`Type`、`TypeAlias`、`Class`、`Enum`、`Record`、`Trait`、`Union` | 保留具体声明类别；Symbol 是这些实体的逻辑角色 |
| 可调用声明 | `Function`、`Method`、`Constructor` | 保留声明身份及其语言类别 |
| 数据成员与其他声明 | `Field`、`Property`、`Variable`、`Constant`、`Macro` | 按源码声明类别区分，归属另由关系表达 |
| 模块绑定项 | `Import`、`Export` | 一项源码绑定或导入/导出规则；多名称语句按项保留，不等同于目标实体 |
| 源码使用 | `Reference` | 一次使用的源码身份；referenceKind 为 calls、references、extends、implements 或 decorates |

关系分类同时定义方向和含义，不能仅凭端点相同就合并：

| 联系 | 当前 RelationKind 与方向 | 保留的区别 |
|---|---|---|
| 路径归属 | `Document / Directory ─in_directory→ Directory` | 直接父目录，与 namespace 归属独立 |
| 源码贡献 | `Document ─declares→ 声明或组织` | 哪份材料贡献了该实体 |
| 词法嵌套 | `Document / 声明 ─encloses→ 声明` | 同一文件内最近的已保留声明层级 |
| 文件组织上下文 | `Document ─in_namespace→ 组织根` | 语言确定的 document 组织归属，根可以是源码声明或合成节点 |
| 语义归属 | `组织 / 类型等所有者 ─contains→ 成员` | 成员可以来自其他文件，与词法嵌套独立 |
| 导入依赖 | `Document / 声明 / Import / Export ─imports→ 导入目标` | 源码项连接目标模块；Document / 声明还保留模块或声明依赖的派生边 |
| 调用与引用 | `声明 / Document ─calls / references→ 目标` | 对象之间的使用关系，保留每次发生的位置 |
| 类型契约 | `类型 ─extends / implements→ 类型` | 继承与实现的不同角色 |
| 使用归属 | `Reference / Import / Export ─occurs_in→ 声明 / Document` | 使用存在于源码中的位置与归属 |
| 使用目标 | `Reference ─references→ 直接目标或 Import` | 当前材料与证据支持的绑定，允许没有目标或多个候选 |
| 名称别名 | `Import / Export ─aliases→ Export / Import / 声明 / 组织` | 保留直接绑定与转导出链；不只保存最终声明 |
| 公开绑定 | `Module / Namespace ─exports→ Export` | 哪个组织公开了该源码项 |
| 修饰应用 | `Reference ─decorates→ 被修饰声明` | 此处应用了修饰；修饰器定义通过 references 单独连接 |

分类是否合理，用以下标准判断：

- **对象身份**：需独立定位、连接或保留生命周期的源码对象才成为节点；名称、范围、签名等描述留作属性。
- **关系语义**：每种关系有明确方向和端点角色；源码贡献、词法嵌套、语义归属与目标绑定分别表达。
- **信息保留**：同位置的不同角色、别名和中间绑定不能因合并或只保留最终目标而消失；目标未知时，已识别的源码事实仍应可查询。
- **查询充分性**：outline、使用位置、依赖链等视图应由图派生；需要回读 Facts 或 AST 才能回答，说明图表达或发布过程存在缺口。
- **能力分层**：分别检查模型能否表达、分析器能否提取、本轮材料能否建立绑定；某个 Kind 存在不代表所有语言都支持，也不代表当前局部图完整。

类别演进需要对照已有代码图的语义及用法：
[colbymchenry/codegraph](https://github.com/colbymchenry/codegraph/blob/6560052a6f856855d3f71eee838fd66ccfa4285d/src/types.ts)
提供具体节点与关系词汇；[lzehrung/codegraph](https://github.com/lzehrung/codegraph/blob/291825b8e054e6d1ffbee03aff999069197fd91b/src/indexer/types.ts)
区分符号、模块索引和导出变体；[Kythe](https://kythe.io/docs/schema/) 区分源码 anchor 与语义实体，
其[模块规则](https://kythe.io/docs/schema/modules.html)说明导入、别名和转导出的联系；
[Joern / CPG](https://cpg.joern.io/) 区分声明、表达式、类型及绑定层。
逐项比较身份粒度、端点方向、属性与关系分工、未解析事实的保留方式，以及实际提取和消费路径，
再说明采用、映射或保留差异的理由。同名不代表同义，例如 CPG 的 BINDS_TO 表达类型实参与形参绑定，
不能直接作为导入绑定链的命名依据；各项目 Kind 的并集也不自动成为本库的模型。

全面性是持续检查程序结构覆盖的方向，不承诺保存完整 AST 或精确模拟运行时。
本轮保留导入项、导出及转导出、修饰应用、未解析基类与接口位置。参数、类型参数、枚举成员的
独立身份，以及 type_of、returns、instantiates、overrides 等关系仍属覆盖检查项；签名文本不能
替代这些关系，也不能将尚未实现的类别列为当前能力。具体语法支持由语言注册项和测试约束。

### Document、Facts、Extractor、Builder 与 Graph

Document 表达一份输入材料：逻辑路径对应文件内容或 gitlink 固定 commit。
DocumentNode 是图中同一 Document 节点的类型化视图；材料类别与 manifest 元数据属于节点属性，
声明的 Module 等实体通过关系连接。清单事实由 CodeGraph 解释，工程 Component 由 repocli 判定。
Builder 从材料路径补齐 Directory，并用固定的 TagRule 集合计算 Document 与 Directory 的直接标签。
Tag 是开放分类词汇，标签不继承、不参与语言解析；消费方根据标签制定排除或处理策略。
路径结构、标签与语言事实共同受构建预算约束，随同一个 Graph 原子发布。
Extractor 负责有界解析并产出 Facts，Facts 保存脱离 AST 的完整单文件材料，包括词法线索、
语言专有证据、候选关系与局部诊断。Facts 的身份由路径和材料内容决定，可以跨快照复用。

Builder 接纳 Facts 和快照专属 ResolutionContext，执行组织、绑定与解析，原子发布只读 Graph。
ResolutionContext 承载调用方已获得的模块根和导入路径证据，候选只与本轮材料集合连接，
其精度限制后续绑定。获取文件、判定工程边界与选择探索范围属于调用方；已支持的清单语义由语言适配器从显式材料提取。

Graph 是一次成功构建的节点、关系和诊断读模型。Builder 补充材料后根据全部已接纳材料重新组织
和绑定，发布新的 Graph：既可以新增节点与关系，也可以修订已有归属、目标集合及证据，替换失效绑定。
已返回的结果保持不变。类型化访问直接读取图事实，Cypher 索引在首次查询时建立。
包级 Build 和 Builder.Build 返回相同的不可变结果；逐批补料由 Builder 的材料提交接口负责。
Graph 类型只提供查询，不持有生产会话，也不返回解析材料。

### 生产与消费边界

生产侧从 Document 提取 Facts，探索依赖并向 Builder 提供解析上下文。Outline 是这一过程中的
辅助结构：适配器按需复用 gotreesitter 的结果，将声明、名称范围与词法嵌套转换为构图材料，
将遗漏信息转换为覆盖诊断，然后释放中间 Outline。Go 可直接从其声明分析获得相同结构事实。

消费侧唯一的代码事实是 Node + Relation。文件 outline 是节点、encloses 与源码位置的投影；
类型成员视图使用 contains，调用路径使用 calls。投影可有自己的结果类型、排序和展示规则，
但不维护另一份事实来源。消费视图缺少信息时，应补足图中的节点、关系或属性，不能回读解析产物补洞。

Facts 的公开检查视图服务生产侧的依赖探索和构图上下文，不作为评审、导航或影响分析的消费输入。
同一应用可以同时承担两侧职责，以 Graph 发布作为事实交接点。BuildReport 提供范围和信息缺口
等分析元数据，说明结论的适用条件，不替代图中的代码事实。

每份材料都有 Document 节点身份；没有解析能力时仍保留材料及其诊断。
因此，局部图中没有某条关系不能证明仓库中不存在关联。
材料身份、接纳与不展开边界见 [Document 契约](document.md)。

### Node、Symbol 与 Namespace

Node.Kind 使用 Document、Package、Module、Class、Function 等具体代码类别，并映射为 Cypher 标签。
Symbol 表达代码声明，Namespace 表达名称与成员组织；二者是可重叠的逻辑角色。
例如 Class 同时承担声明与成员组织，图中仍是同一个 Class 节点。
语言显式 namespace 声明使用具体 Namespace Kind。

`declares` 记录 Document 对声明或组织的源码贡献；`encloses` 记录同一 Document 内
已保留声明的直接词法嵌套；`contains` 记录直接语义归属。
顶层声明由 Document 直接 encloses；嵌套声明由其最近的已保留声明 encloses。Go 方法在文件中
处于顶层，同时通过 contains 归属接收者类型，类型是否在另一个文件不改变源码嵌套。
这三种关系共用声明身份，使文件结构、跨文件组织和符号关系能够一起查询。
Namespace 是实体的成员组织视图，不分配第二份身份。语言规则与显式解析上下文建立组织层级，
例如已知 Go Module 包含 Package、Python Package 包含子包和 Module；局部图允许多个根。
Document 的组织上下文通过 in_namespace 保存，语言适配器决定路径如何参与组织。
祖先与共同归属查询由图关系派生并返回路径证据，消费方据此选择影响或评审边界。身份和嵌套规则见 [命名空间组织](namespaces.md)。

声明的 Location 保存完整声明范围，NameLocation 保存名称 token 或捕获范围；源码顺序由位置确定。
二者独立保留，避免导航时重新搜索名称或把语义归属当作源码层级。
Signature 与 SignatureLocation 保留声明头的原文和位置，表达参数、返回类型、修饰符等静态契约，
实现体单独由声明范围定位。签名文本不等于类型检查结果；兼容性与变更影响由消费者判断。

节点以源码侧身份寻址，图引擎内部数字 ID 不暴露为持久化身份。
相同快照与输入应得到可复现身份；跨版本重命名匹配由消费者另行判断。

### Scope 与 Binding

Scope 表达源码中的可见范围及查找父级；Binding 表达该范围引入的名称、声明来源与类型线索。
Namespace 的成员组织与词法可见性各有含义，contains 不决定名称查找顺序。
BuildScope 表达本轮构建材料集合。

语言实现登记可见性与遮蔽规则，共享索引供引用、调用和接收者线索使用。
普通函数调用复用标识符引用的绑定结果；未提取到词法父级，不能作为顶层可见性的证据。
保留未知局部绑定可以阻止错误的同名回退；未证明的动态行为以局部缺口表达。

### Relation 与 Evidence

Relation 表达一次有向代码关系，分类与方向见上文的关系表。
身份由 Source、Target、Kind 与发生位置组成，位置使用路径及字节范围。
相同端点之间的不同关系或不同调用位置保留为独立边，也允许自递归。

Evidence 记录建立关系的依据：Basis 说明推导规则，Confidence 说明证据强度，
可选 Location 指向支撑语法。同一次发生的多条依据合并、去重并确定性排序，避免最后写入覆盖证据。

| Confidence | 证据语义 |
|---|---|
| `exact` | 在给定材料与已支持的静态语义下，绑定或结构关系已确定 |
| `scoped` | 已有词法绑定、导入、接收者或类型等实际约束，但尚不足以确定绑定 |
| `name_only` | 主要依据名称匹配，未证明名称在该位置的绑定 |
| `heuristic` | 依赖约定、模式或不完整的结构相似性 |

精度顺序为 `heuristic < name_only < scoped < exact`，不按字符串排序。
目标数量与精度相互独立：只有一个同名目标也不能自动升级；多个合理目标继续保留。
Basis 描述“如何得到这条证据”，Confidence 描述“这条证据能证明到什么程度”；
同一种 Basis 可以因材料和约束不同而得到不同档位，不建立 Basis 到 Confidence 的固定映射表。

**`Relation.Confidence` 由 `Evidence[].Confidence` 推导，不是另一份独立判断。**

- 同一关系的独立证据取最高档，并保留全部 Evidence；重复或更多弱证据不会投票升级。
- 一条证据依赖多个必要推导环节时，取这些环节中的最低档；例如导入、转导出、继承查找不能丢失上游的不确定性。
- Confidence 不参与关系身份，调整分级不会制造另一条同位置关系。
- 同一调用或引用位置出现不同目标的 exact 断言时，保留关系并报告 `conflicting_binding`；较低档的备选目标不构成该冲突。

例如，同一调用关系有 name_only 的名称证据和 scoped 的接收者证据，关系投影为 scoped；
导入为 scoped、转导出为 exact 的必要推导链，最终证据仍为 scoped。
无法提出目标的引用保留使用节点及适用的诊断，不虚构关系端点。

confidence 不是概率，exact 也不承诺运行时行为或完整编译器类型检查。
符号间的距离取决于所选关系：词法共同祖先、语义共同所有者和调用路径分别表达不同结构。
通用路径查询提供事实，各消费者选择关系种类及权重。Path 与 Subgraph 保留原始关系及其证据；跨关系的多跳衰减、影响阈值、筛选与排序由消费者决定。

### 源码使用与目标绑定

`payment.charge(order)` 这次调用存在于源码中，与能否找出 charge 的声明是两件事。
Reference 统一表达源码使用，referenceKind 表示其语法角色：calls 保存调用表达式，references 保存
标识符引用，extends / implements 保存类型使用角色，decorates 保存修饰应用。名称、接收者线索和位置随节点提供；receiver 是词法线索，不是已解析类型或限定名。
Reference 通过 occurs_in 指向最内层已保留声明，没有声明包围时指向 Document。
调用表达式和其中的名称引用可以各有节点，因为它们表示不同的源码事实；referenceKind 参与身份，
同位置的不同使用角色也不会合并。Reference 在找到目标后仍保留，不以 Unresolved 命名永久身份。
统一 Reference / referenceKind 的术语参考 [colbymchenry/codegraph 的引用模型](https://github.com/colbymchenry/codegraph/blob/6560052a6f856855d3f71eee838fd66ccfa4285d/src/types.ts#L338)；
这里的归属与绑定由图关系表达。

`references` 从使用节点连接直接绑定：裸导入名先连接 Import，其他已解析使用连接目标实体。
`aliases` 保留 Import → Export → Export / Import → 声明的名称链；沿链查询才能获得最终目标。
声明间的 calls / references、extends / implements 保留已有语义关系，复用同一语言绑定规则与证据。
引用位置与声明依赖是不同查询粒度：统计声明依赖时排除 source.kind = 'Reference'，避免重复计数。

Import / Export 保存每项的名称、位置和 ModuleBinding：specifier、importedName、localName、
exportedName、form、typeOnly。属性描述该源码项；aliases 表达它连接谁。specifier 不是已解析路径，
侧效应导入和通配转导出不虚构本地名。通配项保留导入来源规则，查询具体公开名称时沿模块规则
选取来源的具名 Export 或声明；不会把 `*` 伪装成模块别名。显式导出优先，default 不经通配传播。
Go 未显式命名的 import 只有获得唯一包身份后才补出 localName，不能从路径末段猜包名。

修饰应用使用 referenceKind = 'decorates' 的 Reference，decorates 指向被修饰声明，references
指向修饰器定义或导入绑定；即使修饰器不在图内，应用关系仍为源码事实。它不自动推导路由或
handler 等业务角色。extends / implements 使用也保留独立 Reference，动态基类表达式保留
源码及角色，但不将表达式中被调用的函数误连成基类。

使用和模块项不是声明，不进入 declares / encloses / contains 构成的声明结构，因而不污染 outline。
源码存在和目标可绑定相互独立；同一个绑定链中的局部 references 可以是 exact，而跨模块 aliases
仍是 scoped。每条边保留本环节的证据，消费者不能把局部确定性当作整条链的确定性。

图只组织显式接纳的 Document 及其代码事实，不获取外部源码，也不为缺失目标制造占位符号。
没有 references / aliases 可能来自材料缺失、动态行为或分析能力限制，结合诊断与语言能力判断。
身份取决于源码项而非目标。补入依赖后，同一使用或绑定项可获得新目标，旧边及其精度也可能修订；
已返回的 Graph 保持不变。提取器演进可以扩大覆盖，未提取到某种结构不等于源码中不存在它。

### 声明文档、Marker 与覆盖信息

Documentation 保存绑定到声明的普通文档原文及源码位置，在单文件 Facts 和 Graph 节点中使用同一契约。
语言适配器依据已有语法树识别归属，保留注释定界符和字符串语法；清理、摘要及上下文注入由消费者负责。

spec、case、rule、link、doc 是声明上的结构化意图标记，保留内容及源码位置。
其中 doc 仅来自显式 `+doc` marker；普通文档不会自动成为意图标记。
图提供这些代码事实，消费者决定如何用于评审或检查。

构建或查询的执行失败通过 error 报告；BuildReport 描述已发布范围及局部信息缺口。
诊断标明涉及的材料、声明、关系、上下文或资源，并保留可获得的源码位置。
局部缺口不否定无关事实，也不自动触发消费者的全量回退。

## 主流程与依赖方向

1. 调用方将 Document 交给 Extractor，获得可独立复用的 Facts。
2. 生产侧读取 Facts 探索依赖，把选定材料与 ResolutionContext 交给 Builder。
3. Organize 提供实体与组织贡献；全部实体登记后建立成员索引。
4. Bind 建立导入、类型及接收者归属。所有语言完成 Bind 后，Resolve 才解析引用与调用。
5. Builder 校验端点和预算，原子发布 Graph；构建失败保留上一次成功结果。
6. 消费者只通过图的 Node + Relation 及其查询投影读取代码事实，结合覆盖信息形成业务结果。

阶段屏障使跨文件基类和接收者能在调用解析前进入索引，避免输入顺序决定绑定结果。
具体接口、共享算法与语言接入见 [语言构建流程](language-pipeline.md)。

根包直接定义 Document、Facts、Extractor、Builder、Graph 及公共图值。Extractor 拥有提取
与缓存，Builder 拥有接纳、预算和发布，Graph 拥有不可变结果、类型化访问及延迟查询索引。
异步补料由根包内独立的私有 session 协调，公共对象与自己的状态和行为在同一包维护。

根包调用内部机制，并负责分析材料与公共值、图值与引擎引用之间的转换。internal/graphstore
封装 GoGraph、查询限制及引擎值转换；内部包不依赖根包，也不持有公共 Graph 的生命周期。

internal/analysis 定义分析事实及阶段契约；pipeline 根据语言注册信息编排阶段，语言适配器
提供语义规则。AST 和引擎对象留在适配层，公共 API 返回独立的领域值。

## 关键设计

### 分离提取与快照构建

解析的复用单位是 Facts，关系绑定的单位是 Builder 当前接纳的材料集合。
同一份 Facts 可以进入多个快照；新增、删除或重命名依赖、改变模块配置，都需要重新绑定。
Facts 的只读材料与公开检查视图分离，修改视图不会改变后续构图输入。

Extractor 限制并发、单文件字节和解析时间，可选择调用方持有的有界 ExtractionCache。
显式传递 Facts 不依赖缓存命中。缓存身份包含路径、材料类型和内容，容量不足时照常提取。
共享 Extractor 使多个快照服从同一解析并发上限。

Builder 按材料规模和关系预算接纳与构图，整个构建成功后才替换 Result。
输入上下文取消和执行失败阻止发布；生产侧可通过 AddFailure 保留单文件解析缺口。
Builder 的异步补料接口按需创建独立的私有 session，由它协调解析任务与待发布批次；普通 Builder
只持有已接纳的材料与构图状态。Wait 只等待既有工作，取消等待不会取消构建。
取消与接纳的精确契约见 [使用指南](usage.md)。

### 复用图引擎并保留代码身份

gotreesitter 提供语法解析与事实提取，GoGraph 提供进程内属性图及 Cypher，pond 提供有界任务调度。
CodeGraph 拥有它们之间的代码语义契约，依赖已有能力完成基础工作。

图存储采用有向多重图，按独立 edge handle 写入属性，保留相同端点的不同关系发生。
公开查询只读，节点、关系与路径还原为 CodeGraph 领域值；访问器返回独立副本，防止调用方修改图状态。
Documentation、Marker 与 Evidence 同时提供可过滤属性和完整结构投影，由往返契约验证一致性。

### 有界工作与可解释缺口

构建预算限制材料及事实规模，查询预算限制时间、路径深度和结果规模。
LIMIT 不能独自限制遍历工作量；预算失败不能伪装成完整的空结果。
提取并发由 Extractor 限制；共享一个 Extractor 可统一多快照的解析容量。

grammar 可用、声明可提取、引用可解析是不同能力层次。语言注册项同时声明能力与限制，
仅具备 outline 的语言保留声明并报告关系覆盖缺口。具体支持范围见 [语言能力](language-support.md)。

## 验证边界

库契约测试验证事实、身份、证据、查询与发布不变量；固定语料使用独立参照测量语义覆盖。
消费者的影响结论与执行行为由其自身回归验证，大仓性能需要结合实际材料与消费流程测量。
评测方法见 [真实仓库评测](corpus.md)，历史测量保存在 tests/corpus。

依赖与工具链版本以 [go.mod](../go.mod) 为准；Cypher 支持范围以所锁定依赖及契约测试为准，
不承诺完整 Neo4j 兼容。
