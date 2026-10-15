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

完整图属于生产过程，不另建持久化索引或公开第二张图。Graph、Cypher、Find、namespace 查询与
BuildReport 的节点、关系计数都描述同一个发布结果。Facts 和 FindAsync 是生产侧的提取视图，
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
| 核心节点加依赖路径上的中间节点 | 保留真实路径和证据，普通局部节点不必全部入图 | 规模随依赖密度增长；需要限制候选节点及遍历关系 |

**分析预设选择第四种。** 默认未配置 Admission 时仍发布完整图；显式策略也可以仅选类别。
核心节点选取发生在完整绑定之后。`IncludeDependencyBridges` 控制补入局部变量、字段等中间节点，
不会要求调用方在 AddDocument 时预测跨文件关系。具体准入和路径语义见下文。

### 参考实现与取舍

本次阅读的 colbymchenry/codegraph 版本为 `6421644ef292d0e6decd968e249c18bd648a9078`：

- [索引主流程](https://github.com/colbymchenry/codegraph/blob/6421644ef292d0e6decd968e249c18bd648a9078/src/index.ts#L615)：先提取并写入节点，再初始化 resolver、完成跨文件处理并解析引用；sync 会按新增符号重试此前失败的引用。
- [待解析引用表](https://github.com/colbymchenry/codegraph/blob/6421644ef292d0e6decd968e249c18bd648a9078/src/db/schema.sql#L88)：保存来源节点、名称、种类和位置，用 pending / failed 状态支持后续解析。这是持续索引的实现选择。
- [函数体提取](https://github.com/colbymchenry/codegraph/blob/6421644ef292d0e6decd968e249c18bd648a9078/src/extraction/tree-sitter.ts#L6524)：明确避免普通局部变量节点，将局部类型注解的依赖归到函数，继续遍历初始化调用。它没有为所有局部变量建立完整数据流图。
- [Flow 路径查询](https://github.com/colbymchenry/codegraph/blob/6421644ef292d0e6decd968e249c18bd648a9078/src/graph/named-symbol-flow.ts)：named 模式限制未命名中间符号，directed 模式在固定端点间选最短路径。这发生在查询阶段，不能视为其 Build 使用了通用中间节点准入。

可借鉴的是分阶段绑定、未解析事实保留和有边界的路径选择。CodeGraph 已有完整 Facts 与原子重建，
因此复用自身生命周期；中间节点准入是本库面向阅读与影响分析的选择，不照搬示例仓库的存储结构。
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
这些格式标签与路径标签累加，未知格式不强行归类；Language 与 Gitlink 仍各自保存语法格式和子模块引用。

Builder.Extract、AddDocument 与 AddDocuments 在解析前应用同一材料规则，命中排除时不会查询或污染原始
提取缓存。调用方可通过同一 TagMatcher 的 ExcludedBy 在文档预算之前排除材料。
独立 Extractor 不绑定某个 Builder 的策略；使用它的调用方负责在提取前选材。
外部 Facts 通过 Add 接纳时仍会在构图前应用材料规则，不能绕过准入或改变其它 Builder 的缓存内容。

.gitignore 的解释属于快照拥有者：调用方依据对应版本的规则选材，CodeGraph 不读取工作区、Git 配置
或磁盘目录。材料与符号的语义准入不扩大调用方的 Component、Testset 或 Workset 范围。

## 节点与关系投影

`NodeKinds` 选择初始节点，为 nil 表示所有类别，空集合表示不发布节点；中间节点规则可以补入其它候选类别。分析预设保留：

- 类型定义：Struct、Interface、Class、Type、TypeAlias、Enum、Record、Trait、Union。
- 函数：Function、Method、Constructor。
- 数据声明：模块、包或 namespace 作用域的 Variable、Constant。
- 结构上下文：Document、Directory、Module、Package、Namespace。

`GlobalVariablesOnly` 使用生产侧词法绑定和声明归属，将局部及类型内部的数据声明排除在初始选择之外，
包含匿名函数内部和块级变量。上游 signature 明确识别的函数值声明仍可保留其原有 Variable 类别。
Import、Export、Reference 在预设图中不发布，但完整参与前面的解析与绑定。
局部 Variable / Constant 及 Field / Property 默认不作为核心节点，只有符合中间节点规则时补入。

### 中间节点规则

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
连接子图或枚举所有简单路径，不保证中间节点数量最少。

关系没有独立的发布白名单。选完节点后，保留两端均入图的所有原始关系，保留原 ID、RelationKind、
源码位置、confidence 和 Evidence。用于寻找中间节点的关系集合不限制最终发布的关系种类。
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

回归覆盖跨文件 import 绑定后投影、核心节点间的多步路径、字段中间节点、循环、仅包含关系、
无终点分支、同一核心的自返路径和原始关系证据不变。三个材料的全部六种提交排列与一次性构建
比较最终图，补料场景验证未解析目标变为可绑定目标，以及旧 Graph 不被修改。
