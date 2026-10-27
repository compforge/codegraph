# 分析材料与图准入

CodeGraph 的节点词汇描述它能够理解的代码事实，发布图则描述消费者选择的实体粒度。
局部变量、字段、import binding 和源码使用位置参与名称绑定与关系推导；分析图可以不发布这些节点。
NodeKind、RelationKind 保持原有定义，准入策略独立于语言识别与绑定规则。

## 主流程

```text
调用方提供同一快照的 Documents
  → 按 物料标签及祖先目录判断材料准入
  → 对允许的源码提取完整 Facts；清单提取工程元数据
  → Organize → Bind → Resolve，得到生产阶段的完整实体与关系
  → 按 NodeKind 和作用域选取核心节点及结构上下文
  → 补入连接不同核心节点的局部变量、字段
  → 保留两端均入选的原始关系
  → Build / Wait 原子发布只读 Graph
```

当前实现先构造完整节点与关系集合再投影，该集合只属于生产过程，不另建持久化索引或公开第二张图。
Graph、Cypher、Find、namespace 查询与 BuildReport 的节点、关系计数都描述同一个发布结果。
Facts 和 FindAsync 是生产侧的提取视图，
不承诺其中每个声明都会成为发布节点。

## 构建策略的选择

`AddDocument` 的提交和解析完成顺序都不可控。需要分别决定何时绑定关系、哪些实体进入发布图：
前者保证补料后的语义正确，后者控制消费者看到的粒度。仅仅先放入核心节点，不能解决尚未到达的
定义、import 别名、遮蔽和同名候选问题。生产阶段仍须保留完整绑定事实。

### 何时绑定关系

| 策略 | 乱序与补料行为 | 代价及适用范围 |
| --- | --- | --- |
| 单遍即时连边 | 只连接当时存在的目标，若丢弃未解析引用则依赖输入顺序 | 实现简单，但不适合跨文件分析 |
| 先登记所有声明，再统一绑定 | 同一批内顺序无关；新批到来后仍需重绑旧引用 | 适合有明确批次边界的构建 |
| 待解析引用队列与增量工作列表 | 新符号触发重试；已解析关系也必须随候选变化失效重算 | 适合长期索引，需要反向依赖索引、失效和收敛规则 |
| 累积 Facts，每次对当前快照重新组织、绑定、解析 | 每次发布覆盖当前接纳材料；补料可以修订既有结果 | 复用提取缓存，增加绑定成本，发布与正确性边界清楚 |

**CodeGraph 选择第四种，复用已有阶段屏障。** Builder 累积同一快照的 Facts，Build / Wait 等待本轮
材料完成后执行 Organize → Bind → Resolve，再投影、建查询索引并原子发布。重新构图不等于重新解析
所有文件，提取结果可复用。当前库没有持久化索引，不增加数据库和引用重试队列；只有绑定成本成为
实测瓶颈时，才考虑以保持相同语义为前提引入增量失效。

对相同路径、相同内容及相同 Options 的材料集合，不论提交排列或分批方式如何，最终节点、关系及
Evidence 都应一致。材料不全时允许暂时缺边；补入材料后重新判断目标，不能只追加边。旧 Graph
保持只读，构建失败不会用半张图替换已有结果。

### 哪些实体进入发布图

| 策略 | 优点 | 局限 |
| --- | --- | --- |
| 发布完整图，查询时筛选 | 完整保留导航与细粒度分析能力 | 存储和查询索引大，消费者容易把实现细节全部画出 |
| 只按 NodeKind / 作用域取诱导子图 | 规则简单，图较小 | 会断开依赖局部变量或字段的已有路径 |
| 删除中间节点，向拥有者派生或收缩边 | 核心实体间连接紧凑 | 必须逐种关系定义组合语义；引用链不能直接变成调用 |
| 核心节点加依赖路径上的 bridges | 保留真实路径和证据，普通局部节点不必全部入图 | 规模随依赖密度增长；需要限制候选节点及遍历关系 |

**分析预设选择第四种。** 默认未配置 Admission 时仍发布完整图；显式策略也可以仅选类别。
核心节点选取发生在完整绑定之后。`IncludeDependencyBridges` 控制补入局部变量、字段等桥接节点，
不会要求调用方在 AddDocument 时预测跨文件关系。具体准入和路径语义见下文。

### 参考实现与取舍

对照 colbymchenry/codegraph 的固定版本 `9ee6718ad195c4be422614032a869196de705c5f`：

- [引用模型](https://github.com/colbymchenry/codegraph/blob/9ee6718ad195c4be422614032a869196de705c5f/src/types.ts#L356)与[待解析引用表](https://github.com/colbymchenry/codegraph/blob/9ee6718ad195c4be422614032a869196de705c5f/src/db/schema.sql#L79)：引用记录与 Node 分开。解析成功后生成带位置的 Edge 并删除待解析记录；失败记录保留供 sync 重试。它不为每次使用建立 Reference Node。
- [函数体提取](https://github.com/colbymchenry/codegraph/blob/9ee6718ad195c4be422614032a869196de705c5f/src/extraction/tree-sitter.ts#L7374)：省略普通局部变量节点，将局部类型注解的依赖归到函数，继续扫描初始化调用；具名函数、具名函数值和局部类型仍可保留。它没有为所有局部变量建立完整数据流图。
- [增量重绑定](https://github.com/colbymchenry/codegraph/blob/9ee6718ad195c4be422614032a869196de705c5f/src/extraction/index.ts#L3166)：目标改名或删除后，依据原引用信息恢复待解析任务。这属于持久化索引的生命周期；本库继续复用 Facts 和快照重绑。
- [Flow 路径查询](https://github.com/colbymchenry/codegraph/blob/9ee6718ad195c4be422614032a869196de705c5f/src/graph/named-symbol-flow.ts#L1)：用户点名 A、B 时，已有图中的 `A → helper → B` 可以将 helper 作为 query bridge 展示。helper 原本就是节点，查询没有把它收缩到关系中；这也不决定它是否入图。

可借鉴的是引用事实与节点分离、实体粒度选择和有界路径查询。本库的 dependency bridge 在发布时
决定实体是否入图；上述 query bridge 在查询时选择已有节点组成的路径。两者不能混为同一准入机制。

[Joern slicing](https://docs.joern.io/cpg-slicing/) 和
[CodeQL path queries](https://codeql.github.com/docs/writing-codeql-queries/creating-path-queries/) 也体现了
“完整理解后，按端点和允许的关系选取路径”的思路。它们的数据流能力不等于本库当前的调用、引用绑定能力。

## 材料准入

`Options.Admission` 配置准入，nil 保留完整材料与节点词汇；配置在 Builder 创建时复制。
`ParseTags` 控制符号提取：命中任一标签才解析，nil 不限制，空集合跳过所有普通材料的语法提取。
已识别的 go.mod、package.json、pyproject.toml 仍提取清单元数据，供模块绑定使用。
未解析的普通材料保留 Document 与目录结构，不产生符号或 unsupported_language 诊断。

Document、Directory 的物料分类统一使用 Tags；图中仍分别是 Document、Directory NodeKind，
in_directory、declares 等 RelationKind 保持原义，不另设物料 Kind。
`ExcludedTags` 匹配文件的格式及路径标签和每一级祖先目录的路径标签；命中后既不解析，也不进入发布图。
例如规则 `^vendor$` 只给目录打标签，但其子树仍被准入规则排除。节点自身的 Tags 保存格式分类和直接路径匹配，
不把祖先标签复制到子节点。`excluded_material` 诊断记录排除路径与原因，表示主动选择的范围边界。
普通测试源码可参与分析；测试素材目录由预设排除，消费者可调整标签策略。

`AnalysisAdmission()` 提供源码分析预设：解析 source，排除 dependency、generated、build_output、
cache、minified、tooling 和 test_fixture 材料。开发元数据包括 .git、.idea、.vscode、
.codebase、.devloop、.codex、.claude、.agents、.cursor，以及任意目录下的 AGENTS.md、CLAUDE.md、GEMINI.md。
.gitignore、.dockerignore、.env / .env.* 等常见配置带 config 标签，保留为 Document 上下文，不做符号解析。
普通隐藏目录不因此全部排除。
Markdown/RST 使用 documentation 标签；HTML/CSS、JSON/TOML 等使用 resource 标签；代码格式使用 source。
这些格式标签与路径标签累加；没有任何标签的 Document 以 unknown 兜底，Directory 不追加该标签。
消费者可将 unknown 用于统计或显式排除；它不表示解析失败。Language 与 Gitlink 仍各自保存语法格式和子模块引用。

Builder.Extract、AddDocument 与 AddDocuments 在解析前应用同一材料规则，命中排除时不会查询或污染原始
提取缓存。调用方可通过同一 TagMatcher 的 ExcludedBy 在文档预算之前排除材料。
独立 Extractor 不绑定某个 Builder 的策略；使用它的调用方负责在提取前选材。
外部 Facts 通过 Add 接纳时仍会在构图前应用材料规则，不能绕过准入或改变其它 Builder 的缓存内容。

.gitignore 的解释属于快照拥有者：调用方依据对应版本的规则选材，CodeGraph 不读取工作区、Git 配置
或磁盘目录。材料与符号的语义准入不扩大调用方的 Component、Testset 或 Workset 范围。

## 节点与关系投影

`NodeKinds` 选择初始节点，为 nil 表示所有类别，空集合表示不发布节点；bridge 规则可以补入其它候选类别。分析预设保留：

- 类型定义：Struct、Interface、Class、Type、TypeAlias、Enum、Record、Trait、Union。
- 函数：Function、Method、Constructor。
- 数据声明：模块、包或 namespace 作用域的 Variable、Constant。
- 结构上下文：Document、Directory、Module、Package、Namespace。

`GlobalVariablesOnly` 使用生产侧词法绑定和声明归属，将局部及类型内部的数据声明排除在初始选择之外，
包含匿名函数内部和块级变量。上游 signature 明确识别的函数值声明仍可保留其原有 Variable 类别。
Import、Export、Reference 在预设图中不发布，但完整参与前面的解析与绑定。
局部 Variable / Constant 及 Field / Property 默认不作为核心节点，只有符合 bridge 规则时补入。

### Dependency bridge 规则

Bridge 表示实体在当前节点选择策略中的连接角色，不是新的 NodeKind。核心节点是直接选中的
类型、函数和全局数据声明；bridge 是因此被补充保留的实体；Document 等结构上下文单独保留。
`IncludeDependencyBridges` 沿用此含义。节点仍保留原来的 Variable、Field 等类别，关系仍保留独立端点。

核心端点包括选中的类型、函数和全局数据声明。Document、Directory、Module、Package、Namespace
只提供结构上下文，不作为依赖路径的起点或终点。补入节点须同时满足：

1. 是未入选的局部 Variable / Constant 或 Field / Property；import binding 和源码使用位置不作为候选。
2. 在允许的方向上，从一个核心节点可达，并能到达另一个不同的核心节点；搜索遇到核心节点即停止。
3. 语义步骤使用既有 calls、references、aliases、extends、implements、decorates。encloses / contains
   只允许从核心声明进入其实现节点，不能单独触发补入，也不反向遍历或借目录连接无关代码。

例如 `function handle() { const local = run(); }` 的已有关系为：

```text
handle ──encloses──▶ local ──calls──▶ run
```

当 run 可绑定时，local 随这条路径保留；只有 `const local = 1` 时，单独的 encloses 不使它入图。
同理，`A → field → B` 中的 field 可以保留，但 `A → field` 没有其它核心终点时不会仅因一次引用补入。
这里表达静态代码依赖，不声称存在完整 def-use 数据流或运行时因果链，也不凭连通性制造 handle → run 调用。

实现按正向和反向可达性选取依赖通道，每个候选只需传播至多两个不同核心端点标签，时间和辅助空间
受完整图的节点、边数线性约束。循环不会导致无限遍历；连在通道上的循环也可能保留。这不是求最小
连接子图或枚举所有简单路径，不保证 bridge 数量最少。

关系没有独立的发布白名单。选完节点后，保留两端均入图的所有原始关系，保留原 ID、RelationKind、
源码位置、confidence 和 Evidence。用于寻找 bridges 的关系集合不限制最终发布的关系种类。
因此不提高推断精度、不拼接边，且不会产生悬空关系；消费者仍决定影响传播及渲染规则。

## 使用与预算

```go
policy := codegraph.AnalysisAdmission()
graph, report, err := codegraph.Build(ctx, snapshot, documents, codegraph.Options{
    Admission: policy,
})
```

消费者可修改返回的预设，例如保留生成代码时从 ExcludedTags 移除 GeneratedTag。
同一份完整 Facts 可以供不同 Builder 发布不同节点集合；已发布 Graph 不随补料或策略对象修改而改变。

构建预算仍约束生产阶段的完整事实图，包括投影之前的节点、关系和证据。
投影减少发布结果和查询索引的规模，不承诺减少源码解析或绑定工作，也不会绕过已有预算。

## 验证边界

回归覆盖跨文件 import 绑定后投影、核心节点间的多步路径、字段 bridges、循环、仅包含关系、
无终点分支、同一核心的自返路径和原始关系证据不变。三个材料的全部六种提交排列与一次性构建
比较最终图，补料场景验证未解析目标变为可绑定目标，以及旧 Graph 不被修改。

## 图生成与预算更新方案

以下说明构图职责、预算边界和验证要求；v0.16 的配置迁移见 [使用指南](usage.md#v016-构建预算迁移)。
核心类比见 [对象模型与辅助结构](kernel.md#用解释器理解对象模型与辅助结构)，
内部阶段和数据结构见 [编译式构图方案](language-pipeline.md#编译式构图更新方案)。

### 上游复用是前置约束

CodeGraph 以 gotreesitter 和 GoGraph graph / Cypher 为基础，内部对象划分服从这一分工。
CPython 只提供职责与生命周期的参照，不能据此扩张为自建语法前端、编译框架或图引擎。

| 能力 | 优先复用与 CodeGraph 边界 |
| --- | --- |
| 语法解析、声明及调用等事实提取 | 复用 gotreesitter 的 parser、FactProgram、Outliner 和规则扩展入口；已有输出直接转换，不重复遍历提取同一事实 |
| 名字与类型绑定、跨文件语义证据 | CodeGraph 衔接语言事实及本轮材料；先核对上游覆盖，再保留确有必要的语义补充 |
| 图存储、通用图算法与 Cypher | 复用 GoGraph 的 graph / Cypher 能力；graphstore 负责模型转换、只读及预算边界，不新增平行查询引擎 |
| 实体准入、bridge 语义及发布 | CodeGraph 拥有选择策略执行、稳定身份、证据和发布契约；通用遍历机制优先使用上游或现有实现 |

实现前逐项记录“当前依赖版本的上游能力 → 现有调用点 → 必要缺口 → 改动归属”。上游已覆盖时
直接复用，规则可扩展时优先补规则，确有缺口才增加有界适配。替换成立后同步删除重复实现，
通过语义回归验证；不因已有内部类型而保留重复能力。

### 问题与选择

如果先构造完整公共图、检查预算再执行准入，少量节点的分析图也可能在未准备发布的 Reference
上触限。因此选择发生在装配前，内部分析与公共对象分别计量。减少发布节点不等于减少绑定成本。

考虑三种调整：

| 方案 | 效果与约束 |
| --- | --- |
| 提高完整图预算 | 可以容纳更大的输入，但没有消除未发布节点的物化成本 |
| 将现有预算检查移到投影之后 | 最终图规模可控，但内部分析会失去原来的保护，单独采用不可取 |
| 内部语义分析与公共图生成分开，分别约束预算 | 保留绑定事实，只物化选中节点；符合 Builder 的职责边界，采用此方向 |

内部继续复用 `internal/analysis` 的实体引用、Scope / Binding、候选与关系索引，不建立第二套
公开 Graph、查询接口或持久化索引。辅助结构随 Builder 的分析生命周期管理，不因被命名就成为
新的公共核心对象。标签与粒度策略由消费者选择，CodeGraph 负责执行和报告结果。

### 目标结构与职责

采用编译式构图：Builder 接纳材料，为本轮创建 compilation；Analyzer 产生内部语义结果，
entitySelector 决定公开范围，graphAssembler 生成 Node / Relation，最后由 Builder 发布 Graph。
每个工作对象拥有自己的状态和不变量，阶段通过明确的结果交接。

| 对象 | 改动责任 | 生命周期与交接结果 |
| --- | --- | --- |
| Builder / Extractor / session | 保留材料接纳、提取缓存、调度及最后结果的职责 | 跨调用存在；为每次构建固定输入材料 |
| compilation | 收拢单轮配置、阶段协调和诊断汇总 | 单次构建；交还完整待发布结果 |
| pipeline.Analyzer | 将 pipeline.Build 的语言分组、阶段屏障与 Resolver 管理归入分析对象 | 单次分析；交还 Index 与分析诊断 |
| analysis.SymbolTable | 维护单文件词法作用域、名字绑定与查找职责 | 单文件提取完成后只读，随 Facts 缓存 |
| analysis.Index | 维护实体、成员、本轮绑定候选及证据的统一登记 | 分析期间可写，交给选择及装配时只读 |
| entitySelector | 从内部语义结果选择 core / bridge / context，管理搜索状态 | 单次选择；交还以 Ref 标识的 selection 及选择理由 |
| graphAssembler | 按发布策略生成使用节点及路径上下文，集中处理公开身份、登记、去重、预算及端点校验 | 单次装配；交还 Node / Relation、统计及诊断 |

具体状态、方法及 CPython 对照见 [对象职责](language-pipeline.md#用对象承接工作与状态)。
内部类型采用封装状态的具体 struct；语言能力继续由 Extractor / Organizer / Binder /
RelationResolver 接口提供。Builder 的外部方法与语言接入接口保持既有形状。

```text
AddDocument → 材料准入 → Extractor → Facts（含单文件 SymbolTable）
Build / session → compilation
  → Analyzer：Organize → Bind → Resolve → Index
  → entitySelector：core + bridges + context → selection
  → graphAssembler：selection + 发布策略 → Node + Relation + 报告 → 待发布结果
  → Builder：原子发布 Graph
```

### 策略与兼容边界

材料准入、发布时机和节点选择是不同决策，由 Builder 统一编排，不扩展为任意可插拔流水线：

- 材料准入在提取前决定排除、仅保留上下文或提取源码；已识别清单继续供模块绑定使用。
  外部 Extractor 提交的 Facts 也经过相同接纳规则；材料排除应在接纳预算扣减前完成。
- 语义分析保留正确绑定所需的局部声明、别名、作用域和引用位置。节点策略不提前删除这些事实。
- 节点选择基于绑定后的内部实体与关系，沿用上文 core / bridge 规则，然后生成公共值与查询存储。
  分析模式不物化未入选的 Reference / Import / Export；完整模式仍可发布这些节点。
- 关系保留原始端点、身份、位置、confidence 和 Evidence。缺少目标保留覆盖诊断；需要逐次读取
  未解析使用时，消费者选择包含 Reference 的策略。不能把未发布解释为没有引用或没有影响。

外部发布行为保持既有契约：AddDocument 可以提前完成解析，并通过私有 session 协调构建；
Build 显式执行构建，Wait 等待既有任务。内部为每次构建固定材料集合，使同步和异步路径进入同一套
分析、选择和生成过程。阶段拆分不改变 AddDocument / Build / Wait 的调度与取消含义。

同一 Builder 只接纳同一快照的材料，重复相同材料幂等。补料后根据完整已接纳 Facts 重新绑定，
不能只追加新边；原始提取缓存不存储快照专属绑定。预算失败或构建取消不替换上一份 Graph。
不同节点策略应使用独立 Builder，避免策略变动改变正在执行的发布。

### 分阶段预算与观测

| 阶段 | 约束对象 | 检查位置 |
| --- | --- | --- |
| 输入与提取 | 接纳文档数、单文件与总源码字节、提取并发与解析时间 | 接纳及提取时 |
| 语义分析 | 内部实体与引用事实、绑定候选、语义关系和证据、执行期限 | 各类事实或候选增长时 |
| 图发布 | 最终 Node / Relation 数量 | 生成选中对象时，在查询存储建立之前 |
| 查询 | 遍历工作量、深度、期限与结果规模 | 查询执行时 |

内部分析通过 Options.Analysis 分别约束实体、使用记录、关系、证据及选择工作量。
语言适配器的候选展开沿用剩余证据额度；MaxNodes / MaxRelations / MaxEvidence 只约束最终发布图。
每项额度仍有限，具体字段、默认值和版本迁移见使用指南。最终节点数不能用于推算峰值内存。

BuildReport 的 Nodes / Relations 继续描述实际发布图。Materials 报告提交、接纳、排除及解析失败材料数量；
Analysis 报告实体、使用、关系及证据规模，Selection 报告 core / bridges / context 和搜索工作量，
Publication 报告生成的 Reference 数，各工作阶段分别报告耗时。完整模式中的
源码使用与绑定项单独归类，避免将其误计为 bridges。策略排除、解析或绑定缺口、预算失败分别表达；
错误包含阶段、资源单位、used、adding、limit，日志不输出源码和凭据。

### 实施顺序与改动范围

实现沿工作对象分布，后续修改先核对上游能力，再进入对应 owner：

- `builder.go` / `session.go`：输入接纳及同步、异步生命周期；排除材料在接纳预算扣减前处理。
- `compilation.go`：本轮材料、阶段执行与报告；失败不修改最后发布结果。
- `internal/analysis` / `internal/pipeline`：SymbolTable、Index 的统一登记及 Analyzer 阶段屏障。
- `selection.go`：基于内部 Ref 选择 core / bridge / context，复用有界的双标签 bridge 算法。
- `assembly.go` / `references.go` / `directory.go`：只为入选实体生成公开 ID 与节点，从语义 Edge
  生成关系和可选使用节点；统一登记与发布预算检查，保留解析失败材料的身份及路径上下文。
- `build_limits.go` / `model.go`：独立分析额度与阶段报告；契约及回归场景由相应 owner 的 marker 关联。

repocli impact 显式采用分析粒度，材料范围仍由其仓库捕获策略控制。省略局部实体后，修改行映射到
已发布的所属声明；无法定位时保留文件级保守种子。消费者依赖升级须与预算迁移一起验证，Doctor
通过 repocli 消费结果，不维护另一套图裁剪。

### 验收与交付

| 验收面 | 必须证明的行为 |
| --- | --- |
| 上游复用 | 语法事实继续来自 gotreesitter；图存储和 Cypher 继续经现有 GoGraph 适配；复用替换后无平行提取、存储或查询路径 |
| 对象边界 | 选择不修改 Index / SymbolTable；selection 只描述实体及理由；使用节点和路径结构由 assembler 按策略生成；发布图不借用可变分析状态 |
| 符号表 | 既有语言的嵌套作用域、同名遮蔽、查找父级及无声明目标绑定保持等价；复用 Facts 和补料不改写单文件表或泄漏跨构建绑定 |
| 结果等价 | 固定语料中，完整模式与重构前的节点、关系、身份、位置、证据和诊断一致；分析模式与原准入策略结果一致 |
| 使用位置语义 | 同名不同位置的使用不合并；未解析使用保留既有诊断，完整模式保留其 Reference；decorates、aliases 等关系保持既有表达 |
| bridge 选择 | 覆盖多步路径、循环、无另一核心终点、自返路径、字段、局部变量与函数值；保留原始边，不合成直接 calls |
| 有界构建 | 大量引用而少量发布节点时不消耗发布节点额度；分析、bridge 搜索、发布各自触限都明确失败，上一份 Graph 不变 |
| 补料与确定性 | 乱序、分批和一次性提交在相同最终材料下结果一致；补料修订绑定，旧 Graph 不变；失败材料仍保留 Document 身份 |
| 调度与取消 | 同步和异步路径使用同一构建过程；取消提取、取消构建、仅取消 Wait 保持各自契约；外部 Facts 也遵守准入 |
| 消费者正确性 | repocli 回归局部声明修改的种子映射、受影响文件及测试选择；覆盖无法确定映射时的保守处理 |
| 成本 | 固定语料比较完整及分析模式的阶段计数、耗时、分配和峰值内存；验证省略的使用节点未被先物化再删除 |

CodeGraph 执行 make lint test build，并运行受改动影响的 Go / Python / TypeScript 语义与固定语料评测。
repocli 在其仓库执行对应 impact 回归及项目验证。交付记录代码版本、语料、策略、预算与阶段报告，
分别说明语义等价、预算行为和实测成本；命令成功或节点变少不能单独证明影响分析正确。

本方案不增加运行时执行、完整数据流分析或持续数据库索引。性能目标是减少不必要的公共对象构造
与查询索引规模；是否降低整体耗时和峰值内存，以分阶段测量为准。


### 固定 Doctor 语料测量

使用项目固定的 Doctor `2c6dd6edaf045ce126f3ad176d5036e68407561c` 源码，选取评测 profile 的
四个源码目录中 575 个非声明 TypeScript 文件。比较 CodeGraph `50c5a1a` 与本次实现，使用相同
AnalysisAdmission、500000 节点和 1000000 关系额度，独立进程顺序运行三组；下表为中位数。

| 指标 | 构图后裁剪 | 选择后装配 |
| --- | ---: | ---: |
| 发布节点 / 关系 | 9163 / 55712 | 9163 / 55712 |
| 构建耗时 | 1.226 s | 0.753 s |
| 构建累计分配 | 1.568 GB | 1.185 GB |
| 进程峰值 RSS | 720 MB | 636 MB |

六次运行的节点、关系和诊断序列 SHA-256 均为
`1230832977712646063602d4a2afd73ce59c228d14342427837e99371c1d706d`。
将发布节点额度改为 10000 时，旧实现触限，新实现仍发布相同的 9163 个节点且不生成 Reference。
新实现分析了 25185 个内部实体、114981 条使用记录及 111442 条关系。

累计分配覆盖 Build；峰值 RSS 通过 macOS time -l 记录，包含输入加载及结果摘要计算。
这些数值只描述该固定语料和本机测量，不是跨机器性能承诺；图语义仍由独立参照与回归验证。
