# 图模型、Kind 与 lowering

`pkg` 承载独立于解析器与图引擎的公开值模型，包括 NodeKind、RelationKind、Node、Relation、Location、
Tag、Confidence 和报告。根包通过 alias 保留导入兼容；`pkg` 不依赖 `internal`。
两个 Kind 都是 string 定义的开放 enum。internal 可以定义相同类型的常量，只有发布入口检查公共词表。
Go 标识符大小写控制包可见性，序列化字符串的大小写不承担阶段或权限语义。


## 静态代码与图的对应

Graph 是输入代码在给定快照和分析范围内的静态表示。可以用下面的对应理解它保留的对象与联系：

| 静态代码 | Graph 中的表达 |
|---|---|
| Repository | Graph；可以只覆盖仓库的一部分材料 |
| 物理目录 | Directory；由已接纳材料的路径补齐祖先层级 |
| 语言组织结构 | Namespace（逻辑角色）；按语言规则表现为 Package、Module 等节点 |
| File 或 Git 材料条目 | Document；普通源码、go.mod 等文件以及 gitlink 都有材料身份，按材料种类与分析能力处理 |
| Class、Interface、Field、Function 等声明 | Symbol（逻辑角色）；节点保留具体语言类别 |
| 源码中的调用表达式与标识符使用 | 具体 occurrence Kind；目标不在当前图中也有源码身份 |
| 导入、导出及转导出项 | Import / Export；保留每项名称、别名、模块说明符与位置 |
| 源码中的包含、归属、调用、引用等联系 | Relation；不同关系保留各自的语义和来源依据 |
| 声明、名称或关系发生的文本位置 | Node / Relation 上的源码位置属性 |

目录与 Namespace 不固定一一对应：一个包可以由多个文件共同组成，一个文件也可以声明嵌套的
逻辑组织。Document 提供材料边界，Symbol 提供声明身份，Relation 把源码组织与语义联系接起来；
章节式的大纲是这些事实的一种读取方式。

## NodeKind 与 RelationKind

NodeKind 与 RelationKind 是代码到图的映射词汇，定义模型能保留哪些事实。评估模型时，既要检查
职责是否清楚，也要检查这些分类是否全面、语义是否合理：只靠现有节点与关系，消费者能否还原
所需的程序结构，能否区分含义不同的联系。消费需求暴露的信息缺口应回到这一层审视。

当前节点按所表达的代码对象分组如下；分组用于解释语义，不增加图标签或新的节点身份。
完整枚举的代码定义见 [pkg/kinds.go](../pkg/kinds.go)，各语言实际提取范围见 [语言能力](language-support.md)。

| 对象 | 当前 NodeKind | 分类依据 |
|---|---|---|
| 输入材料 | `Document` | 已接纳材料的身份；Tags 表达 source、documentation、resource、manifest 等可叠加物料分类；Language 表达 grammar，Gitlink 保存子模块引用 |
| 路径结构 | `Directory` | 已提供材料的祖先目录，按路径去重，Tags 直接匹配自身路径 |
| 名称与成员组织 | `Package`、`Module`、`Namespace` | 语言中的组织实体，不将物理目录直接等同于命名空间 |
| 类型声明 | `Struct`、`Interface`、`Type`、`TypeAlias`、`Class`、`Enum`、`Record`、`Trait`、`Union` | 保留具体声明类别；Symbol 是这些实体的逻辑角色 |
| 可调用声明 | `Function`、`Method`、`Constructor` | 保留声明身份及其语言类别 |
| 数据成员与其他声明 | `Field`、`Property`、`Variable`、`Constant`、`Macro` | 按源码声明类别区分，归属另由关系表达 |
| 模块绑定项 | `Import`、`Export` | 一项源码绑定或导入/导出规则；多名称语句按项保留，不等同于目标实体 |
| 源码使用 | `CallSite / ReadSite / WriteSite / TypeUse / CaptureSite / ResourceUse / DecoratorUse` | 一次语义使用的源码身份 |

关系分类同时定义方向和含义，不能仅凭端点相同就合并：

| 联系 | 当前 RelationKind 与方向 | 保留的区别 |
|---|---|---|
| 路径归属 | `Document / Directory ─in_directory→ Directory` | 直接父目录，与 namespace 归属独立 |
| 源码贡献 | `Document ─declares→ 声明或组织` | 哪份材料贡献了该实体 |
| 词法嵌套 | `Document / 声明 ─encloses→ 声明` | 同一文件内最近的已保留声明层级 |
| 文件组织上下文 | `Document ─in_namespace→ 组织根` | 语言确定的 document 组织归属，根可以是源码声明或合成节点 |
| 语义归属 | `组织 / 类型等所有者 ─contains→ 成员` | 成员可以来自其他文件，与词法嵌套独立 |
| 导入依赖 | `Document / 声明 / Import / Export ─imports→ 导入目标` | 源码项连接目标模块；Document / 声明还保留模块或声明依赖的派生边 |
| 调用与引用 | `声明 / Document ─calls / reads / writes / uses_type / captures→ 目标` | 对象之间的使用关系，保留每次发生的位置 |
| 类型契约 | `类型 ─extends / implements→ 类型` | 继承与实现的不同角色 |
| 使用归属 | `Occurrence / Import / Export ─occurs_in→ 声明 / Document` | 使用存在于源码中的位置与归属 |
| 使用目标 | `Occurrence ─binds_to→ 直接目标或 Import` | 当前材料与证据支持的绑定，允许没有目标或多个候选 |
| 名称别名 | `Import / Export ─aliases→ Export / Import / 声明 / 组织` | 保留直接绑定与转导出链；不只保存最终声明 |
| 公开绑定 | `Module / Namespace ─exports→ Export` | 哪个组织公开了该源码项 |
| 文件引用 | `所属声明或 Document ─uses_resource→ Document` | 基于显式资源语义建立的文件关联；Resource 保留表达式与路径，不推断读写行为 |
| 修饰应用 | `DecoratorUse ─decorates→ 被修饰声明` | 此处应用了修饰；修饰器定义通过 binds_to 单独连接 |

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
图保留导入项、导出及转导出、修饰应用、未解析基类与接口位置。参数、类型参数、枚举成员的
独立身份，以及 type_of、returns、instantiates、overrides 等关系仍属覆盖检查项；签名文本不能
替代这些关系，也不能将尚未实现的类别列为当前能力。具体语法支持由语言注册项和测试约束。

## 分析事实与 lowering

```text
source facts → lexical binding / resolution → lowering → admission → graph
                  intermediate operations       semantic relations
```

NameUse、CallTargetUse、CompositeKeyUse、ReadWriteUse 等中间操作参与解析，不直接成为图的 Kind。
遮蔽记录即使没有声明目标，也必须保留，防止错误绑定同名外层声明；多个已保留的词法候选仍参与解析。

| RelationKind | 含义 | 例子 |
|---|---|---|
| Calls | 调用目标 | `handler()` |
| Reads | 读取值或传递函数值 | `register(handler)` 的 handler |
| Writes | 写入可解析存储 | `state.value = x` |
| UsesType | 类型依赖 | 参数类型、类型转换、构造的类型 |
| Captures | 嵌套命名 callable 使用外层局部存储 | inner → outer.value |
| UsesResource | 显式 API 语义证明的文件关联 | 路径表达式 → Document |
| Aliases | 名称绑定链 | Import → Export → declaration |
| BindsTo | occurrence 的直接解析目标 | CallSite → declaration 或 Import |

`x += y` 对 x 产生 Reads 和 Writes。callee 已产生 Calls 时不再重复产生 Reads；不支持的动态调用
仍可保留已证明的值依赖及诊断。Reads / Writes 不表示文件 I/O；UsesResource 不推断文件读写效果。
Captures 与读写证据共存，admission 保留捕获的存储目标。Python 只提升被命名嵌套函数捕获的局部绑定，
未捕获且不受声明提取支持的局部仍是分析记录。匿名函数调用、Python global/nonlocal 仍受现有能力限制，
不凭 Kind 的存在承诺全语言覆盖。

发生点使用 CallSite、ReadSite、WriteSite、TypeUse、CaptureSite、ResourceUse、DecoratorUse。
`OccursIn` 表达归属，`BindsTo` 表达目标；语义依赖从所属声明或 Document 出发。
精简图不创建发生点，依赖关系无需借助中间节点才能成立。

预算分开计量：RawUses 默认 4,000,000，约束全部语法 use；Uses 默认 1,000,000，约束可参与语义解析的
候选（包括尚未绑定的名字，不包括已知仅遮蔽的局部名字）；Relations / Evidence 约束降低后的内部关系
和证据，MaxNodes / MaxRelations / MaxEvidence 约束实际发布。额度有限且失败原子，拒绝材料或不发布
发生点均不会关闭分析预算。BuildReport.Analysis 同时报告 RawUses 和 Uses。

迁移时将旧 `references` 查询按目的改为具体语义关系，源码导航改用 BindsTo。旧 Reference / References
常量保留以兼容编译，但 Builder 不再产生这两种图对象。`ReferenceKind` 字段仍可读取 occurrence 的角色。
依赖消费者应覆盖所有语义依赖；可使用 `RelationKind.IsDependency()`。BindsTo 不属于声明依赖。

Corpus 的历史 oracle 统计 identifier binding，测试对照层将细分边映射到这一评测口径，并用原始 callee
位置对照 Calls；原始观察产物保留新 Kind。该口径验证目标与位置，不验证操作分类；操作分类由独立语义
fixture 验证，不能把两类指标混为一谈。

## 固定基线验证

对 gotreesitter `ba95e4ea1b33276c6aa5f1174338adcab39be039` 使用相同输入和 AnalysisAdmission，
比较改动前后首个快照的分析、发布及依赖集合。原版仅临时提高 Uses 才能完成测量；新版使用默认额度。

| 指标 | 原版 | 新版 |
|---|---:|---:|
| 原始 use | 1,552,054 | 1,552,099 |
| 语义候选 use | 与原始 use 混计，超过 1,000,000 | 961,668 |
| 内部关系 | 833,621 | 704,470 |
| 公开节点 | 48,512 | 48,513 |
| 公开关系 | 650,594 | 518,105 |
| Calls | 146,280 | 146,280 |

Calls 的端点、类型、文件及 byte span 集合完全一致。旧版全部 502,105 条 calls / references，
均可在新版同端点、同位置语义关系中找到，或由同端点且覆盖 callee 位置的 Calls 承接，无依赖丢失。
公开关系减少约 20.4%；新增节点是捕获所需的局部存储，并产生 4 条 Captures。
该测量验证语义与额度，不是峰值内存或性能基准。
