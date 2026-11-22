# 材料准入与发布范围

`Options.Admission` 决定哪些材料参与分析，以及哪些节点和关系对调用方可见。前一个决定影响可用的绑定证据，后一个决定控制图的粒度。

## 主流程

1. Builder 按材料和祖先目录的标签决定排除、保留 Document 或提取源码 Facts。
2. 对本轮接纳的材料统一执行 Organize、Bind、Resolve。局部变量、字段和导入项都可以参与绑定。
3. 在绑定后的内部 Index 上选择核心实体、结构上下文，并按策略补入依赖路径上的 bridge。
4. 装配入选的公开节点、两端入选的原始关系及所需的源码使用位置。校验端点和发布预算后，Builder 原子发布只读 Graph。

补料后，Builder 可复用单文件 Facts，并对当前材料重新绑定。同一材料集合及配置的最终结果与提交顺序无关。
新增材料可以修订已有目标和证据。构建失败或取消时，调用方仍可读取上一份成功发布的 Graph。
阶段职责与调用示例见[构图流程](build-pipeline.md)。

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
Import、Export、occurrence 在预设图中不发布，但完整参与前面的解析与绑定。
局部 Variable / Constant 及 Field / Property 默认不作为核心节点，只有符合 bridge 规则时补入。

### Dependency bridge 规则

Bridge 表示实体在当前节点选择策略中的连接角色，不是新的 NodeKind。核心节点是直接选中的
类型、函数和全局数据声明；bridge 是因此被补充保留的实体；Document 等结构上下文单独保留。
`IncludeDependencyBridges` 沿用此含义。节点仍保留原来的 Variable、Field 等类别，关系仍保留独立端点。

依赖路径的端点包括选中的类型、函数和全局数据声明。结构上下文通常不作为端点；已入选且被 `uses_resource` 指向的 Document 也可作为资源依赖端点。路径候选须满足以下条件。

1. 是未入选的局部 Variable / Constant 或 Field / Property；import binding 和源码使用位置不作为候选。
2. 在允许的方向上，从一个核心节点可达，并能到达另一个不同的核心节点；搜索遇到核心节点即停止。
3. 语义步骤使用 calls、reads、writes、uses_type、captures、uses_resource、aliases、extends、implements。encloses / contains
   只允许从核心声明进入其实现节点，不能单独触发补入，也不反向遍历或借目录连接无关代码。

直接由核心实体通过 `captures` 捕获的候选也会保留，即使捕获路径只有这一个核心端点。

例如 `function handle() { const local = run(); }` 的已有关系为：

```text
handle ──encloses──▶ local ──calls──▶ run
```

当 run 可绑定时，local 随这条路径保留；只有 `const local = 1` 时，单独的 encloses 不使它入图。
同理，`A → field → B` 中的 field 可以保留，但 `A → field` 没有其它核心终点时不会仅因一次引用补入。
这里表达静态代码依赖，不声称存在完整 def-use 数据流或运行时因果链，也不凭连通性制造 handle → run 调用。

实现按正向和反向可达性选取依赖通道，每个候选只需传播至多两个不同核心端点标签，时间和辅助空间
受内部 Index 的实体、关系数量线性约束。循环不会导致无限遍历；连在通道上的循环也可能保留。这不是求最小
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

| 额度 | 计量范围 |
| --- | --- |
| 材料与提取 | 接纳文档数、单文件及总字节、解析并发和单文件解析期限。 |
| `Options.Analysis` | 本轮内部实体、原始使用记录、候选使用、关系及证据。`SelectionSteps` 限制选择与 bridge 传播的工作量。 |
| `MaxNodes`、`MaxRelations`、`MaxEvidence` | 实际生成的公开节点、关系和证据。省略 CallSite 等节点可以降低发布成本，对应的调用事实仍计入分析额度。 |

BuildReport 的 Materials、Analysis、Selection 和 Publication 分别记录各阶段的规模，Nodes 和 Relations 是最终发布数量。
查询另有独立额度。预算字段与用法见[使用指南](usage.md#v016-构建预算迁移)，RawUses 和 Uses 的口径见[图模型与 lowering](semantic-kinds.md)。

## 验证边界

回归覆盖跨文件 import 绑定后投影、核心节点间的多步路径、字段 bridges、循环、仅包含关系、
无终点分支、同一核心的自返路径和原始关系证据不变。三个材料的全部六种提交排列与一次性构建
比较最终图，补料场景验证未解析目标变为可绑定目标，以及旧 Graph 不被修改。
