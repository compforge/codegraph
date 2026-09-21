# CodeGraph 内核设计

状态：已实现进程内图内核、多语言源码适配与通用声明提取，尚未首次发布或接入消费者。

## 定位与概念

CodeGraph 是代码特化的属性图 Go 库：从指定源码范围提取文件、代码声明及其关系，
提供可追溯、带置信依据的关联查询，供代码评审、影响分析等消费者使用。

CodeGraph 在进程内直接依赖 gotreesitter 与 GoGraph。gotreesitter 提供语法解析与可用的
事实提取能力，GoGraph 提供内存属性图及 Cypher 执行能力；代码语义由 CodeGraph 拥有。

### Graph、Node 与 Relation

- Graph 对应一个源码快照下已构建的局部图。范围外的文件不等于不存在关系。
- Node.Kind 直接表达 File、Struct、Interface、Field、Method、Function、Type、TypeAlias 等具体类别。
- Relation 即有向 Edge，Kind 包括 contains、imports、calls、references、extends、implements。
- Relation 有独立身份，允许递归自环，以及相同端点之间不同关系或不同调用位置的多条边。
- Path 与 Subgraph 沿用图的概念，保留参与查询的节点、关系及其证据，不只返回文件名集合。

节点使用源码侧身份，GoGraph 内部 ID 不作为消费者持久化的符号身份。身份在同一快照和相同输入下
应可复现；跨版本重命名匹配是另一个问题，不能由内部数字 ID 推断。

### Document 与输入边界

Document 表达提供给图的一份源码材料，由快照内的逻辑路径与完整源码内容组成。来源可以是文件系统、
Git revision 或内存；获取材料与选择材料由消费者负责，提取代码事实和解析关系由 CodeGraph 负责。
路径提供稳定身份、语言识别和相对 import 上下文，不要求实际文件存在。快照身份统一归 Graph 所有。

Document 属于构图输入，File 与具体声明类别属于图节点。读取一个 Document 后，图可以包含对应的
File、Function、Struct 等节点及其关系；输入材料本身不增加一个 Document 节点类别。review unit、
候选测试、changed 等消费策略不进入 Document。

`AddDocuments` 接收显式批次，仅在这些材料与已加载事实之间解析关系；缺少依赖时保留覆盖诊断，
补充材料后重新解析。源码读取、Git 版本选择与依赖枚举属于消费者的输入准备职责，CodeGraph 不隐式
扫描文件系统或获取依赖。没有注册 grammar 的 Document 仍进入图：只产生对应的 File 节点，不触发解析、
不产生声明与关系，覆盖缺口以 `unsupported_language` 诊断保留在构建报告中；消费者不能将"没有符号"
误解为"文件不存在"。材料选择（是否纳入纯文本、图片等非代码文件）仍是消费者职责。

输入内容在调用期间必须保持不变；图保存独立的字节副本，避免后续批次重建受到调用方内存修改影响。
逻辑路径相同且内容相同的输入幂等；已加载路径或同一显式批次内的内容冲突回滚该批次。

`Extract` 提供不入图的事实提取：返回与 `AddDocuments` 消费的同一份声明、import、调用与诊断事实，
供消费者在决定构图范围前探索依赖。结果按路径与内容身份缓存，后续同内容 `AddDocuments` 直接复用，
同一份源码在探索与构图之间不会解析两次；已加载 Document 的事实由图保留，投影不再需要解析。
import 事实记录被导入的名称（from-import、JS/TS 具名导入与 re-export，空表示整模块），export 别名
以公开名到本地名的映射记录；两者是供消费者做模块解析的事实，图内关系解析规则不变。
Python 源码额外记录语句级上下文事实：保持执行顺序与作用域的语句列表及其受限表达式词汇，
消费者自行解释（如搜索路径推断），图模型不依赖语句级事实。该事实以语言中立的 `Statements`
结构承载，当前仅为 Python 捕获，其他语言未实现捕获时为空。import 事实同时记录引入词法作用域的
绑定名。Go 源码中的 `//go:embed` 指令以 `unsupported_resource` 诊断记录，不解析嵌入资源。

### 具体类别与声明归属

每个节点的具体 Kind 是唯一的图分类，同时映射为 `kind` 属性和 Cypher 标签；symbol 仅作为代码
声明的统称，不形成上位图标签或第二套分类字段。通用遍历使用无标签节点模式，精确查询使用具体标签。
公开类别表达代码语义，不照搬底层 AST 类型；新增类别与语言提取能力一起声明和验证。

Struct、Interface 表达直接以对应语法声明的类型；Type 表达 `type ID int` 等其他命名类型，
TypeAlias 表达显式别名，不因右侧是结构体而丢失别名语义。没有类型检查证据时不推断底层类别。

字段和接口中的显式方法有独立节点身份、位置、marker 与关系。`contains` 的 `declaration` 依据
表达词法归属：文件包含顶层声明，结构体包含字段，接口包含声明的方法，函数包含局部类型声明。
同组字段分别成节点，不互相包含；内嵌结构体字段按其类型的非限定名称命名。

接收者方法保留文件词法归属，并通过 `receiver_declaration` 依据关联已加载的包级接收者类型。
类型与方法可以跨文件；嵌套局部同名类型不参与匹配。唯一目标为 exact，多个目标为 candidate 并记录
歧义，缺少目标记录未解析诊断。关系位置指向方法声明，不指向接收者类型所在文件。该关系只说明
声明归属，不代表已解析接收者调用或已验证编译器类型约束。

### Marker 与关系置信依据

spec、case、rule、link、doc 是 CodeGraph 的核心 MarkerKind，作为声明节点的结构化属性存在。
Marker 保留内容与源码位置；不会因使用通用图引擎而变成某个消费者私有的约定。

Relation 的 confidence、判定依据和来源位置是关系属性。能够提出候选目标时，可以形成带依据的
候选关系；无法提出目标的引用保留为构建诊断，不虚构目标节点，也不当成已解析关系。
Resolution 表示确定引用目标的过程，不作为与 Graph、Node、Relation 并列的核心对象。

confidence 使用 exact / candidate：前者是已加载范围内的唯一语法绑定或显式结构关系，后者表示
可能目标（目标可能只有一个，但仍缺少运行时路径等证据）。它不是概率，也不等同于通过编译器类型检查。
不确定、未解析和动态调用均进入构建报告。

## 代码结构

公共类型位于根包，底层依赖限制在内部适配层。

```text
codegraph/
├── go.mod
├── README.md
├── AGENTS.md
├── graph.go                   # 公共 Graph 入口与生命周期
├── node.go                    # Node、具体 NodeKind 与源码属性
├── relation.go                # Relation、RelationKind、置信依据
├── marker.go                  # Marker、MarkerKind 及源码绑定信息
├── document.go                # Document 源码材料与显式批次输入
├── facts.go                   # Extract 不入图事实提取与内容身份缓存
├── build.go                   # 构图输入、范围、预算与构建报告
├── build_graph.go             # 从事实构建节点/关系并物化发布批次
├── query.go                   # 只读 Cypher 查询、参数及图结果
├── internal/
│   ├── extract/               # gotreesitter 适配与语言事实提取
│   │   ├── extract.go
│   │   ├── languages.go       # grammar 识别与声明类别映射
│   │   ├── outline.go         # 通用 outline、Python/JS/TS 事实与 marker
│   │   ├── bindings.go        # 局部调用的遮蔽检查
│   │   ├── go.go              # Go 词法绑定与 marker 文档归属
│   │   └── go_declarations.go # Go 声明分类、成员与词法归属
│   ├── resolve/               # 作用域、import 与引用目标解析
│   │   ├── resolve.go
│   │   ├── modules.go         # 源码模块路径候选及本地调用
│   │   └── receiver.go        # 包级接收者类型索引
│   └── graphstore/            # GoGraph 适配、属性编码、边身份与结果转换
│       ├── store.go
│       └── policy.go          # 使用上游 AST 检查路径边界
├── graph_test.go              # 真实解析/查询的内存源码夹具与契约测试
├── file_only_test.go          # 无 grammar Document 的 File 节点与身份/预算契约
├── node_kinds_test.go         # 具体类别、成员与跨文件归属契约
├── example_test.go            # 可执行使用示例
├── Makefile                   # 格式、静态检查、race 测试和编译入口
└── docs/
    └── kernel.md              # 模型、主流程与关键设计依据
```

根包就是公共 codegraph API，不再嵌套同名包。测试与被测代码放在一起。
internal 按实际职责组织，不预建多后端框架，也不引入 cmd、server 或独立数据库进程。
内部包交换语法事实或通用图值，不反向导入根包；由根包完成代码领域实体的组装。

## 构图与查询流程

1. 消费者提供源码快照标识、Document 批次、允许范围及预算。diff 是入口来源之一，
   不是图必须认识的业务对象，也不是唯一构图方式。
2. CodeGraph 在范围内消费源码材料，通过 gotreesitter 提取声明、引用、import 和注释事实。
   适配层在释放语法树之前保留独立的事实与位置。
3. 构建具体类别节点及 contains 等已知关系，将 marker 绑定到所属声明节点。
4. 结合语言作用域与依赖规则解析引用目标，构建带置信依据的关系，记录未解析和范围受限诊断。
5. 通过 Go API 将节点与关系写入内嵌 GoGraph，完成当前构建批次，再提供一致的只读查询。
6. 消费者使用 Cypher 查询关联节点、关系、路径或子图，结合构建覆盖情况形成业务结果。

图可以从空图开始按范围补充 Document；重复加入同一快照的同一事实应幂等，不因重复加载制造多重边。
真实的不同调用位置则必须保留。before / after 使用各自快照的图，避免混合版本事实。
同一 Graph 的构建批次串行执行，批内按 `Options.BuildConcurrency` 有界并行提取 Document：
`0` 默认取 `min(GOMAXPROCS, 4)`，`1` 串行，正整数指定上限，负数无效。
worker 独占 parser、AST 与事实结果，不写共享节点、关系或图存储；协调者按输入顺序合并结果。
全部声明就绪后统一解析跨文件关系、写入 GoGraph，成功物化后原子发布；查询可继续使用上一批次。

调度窗口同时受并发数、剩余文件数和源码字节预算限制。正在解析的材料先预留容量，解析失败后
释放预留，再调度后续材料；不能因 worker 完成顺序改变预算结果。单文件解析失败仍为覆盖诊断，
预算、身份冲突和取消则回滚整批。取消后等待已启动 worker 退出，不留下后台解析任务；单次解析仍受
ParseTimeout 约束。并发上限属于每个 Graph，不是整个进程的全局容量限制。

新增材料会复用已提取事实并重建
当前局部图，不承诺底层增量更新性能。调用者负责 Document 的来源一致性及访问边界。
并行度的性能收益需结合解析、关系解析和物化的成本衡量；`BenchmarkDocumentBuild` 对相同混合语言
材料比较 1/2/4/8 worker 的完整构建耗时与分配量，不代表大仓或消费者端到端性能。

## 关键设计

### 复用内嵌图引擎

GoGraph 可直接 import，在同一进程内构造有向多重属性图并执行 Cypher，无须服务器或强制落盘。
图配置使用 Directed、Multigraph；Weightless 表示不使用算法边权重，不影响 confidence 等属性。

Go API 创建关系使用独立 edge handle，并按 handle 设置类型与属性。按端点对设置属性不能区分
平行调用边。建图不需要将提取结果拼成 Cypher 写入语句。

选择 GoGraph 的关键依据是内存生命周期及实际验证过的多段、变长路径与逐边属性查询。
goraphdb 更偏持久化数据库，其当前普通 MATCH 执行与变长路径语义存在限制，不作为本方案底座。

### 保留代码语义，复用 Cypher

公开模型保持 Graph / Node / Relation 的图概念；具体 NodeKind 映射到节点标签，RelationKind
映射到关系类型。调用者面对的是稳定的代码图语义，gotreesitter AST 与 GoGraph 内部对象留在适配层。

查询使用参数化 Cypher，不自造查询语言。提供节点、关系和路径的结果投影，而不是只提供若干固定
的 FindCallers 类封闭查询。公开查询只读，通过图引擎只读执行入口限制写语句。

常见的消费者定位不必拼接内部 ID：`Find(path, kind, qualifiedName)` 按源码位置返回声明，
`Node` 按源码身份读取单节点，`RelationsFrom` / `RelationsTo` 提供有界邻接。它们与 Cypher
共享同一 detached 投影和排序语义；消费者可以把 diff 的行范围映射到 `Location.EndLine`，
不需要再次解析 AST 或复制 CodeGraph 的身份算法。

例如，查询两跳以内、每条边均符合条件的调用路径：

```cypher
MATCH p = (a {id: $nodeID})-[:calls*1..2]->(target)
WHERE all(r IN relationships(p) WHERE r.confidence = 'exact')
RETURN target, p, [r IN relationships(p) | r.line] AS lines
```

实体返回值为公共 Node / Relation / Path；路径中的关系保留存储方向，节点顺序表达遍历方向。

GoGraph 属性支持标量、时间、字节和列表，不支持原生嵌套 Map。Marker 的领域结构与可查询属性投影
分别定义：markers 是种类列表，各种类属性为内容列表，markerData 是携带完整位置的 JSON。
这些投影由同一写入入口维护，并以内容与位置往返测试约束一致性。

### 构建范围与结果完整性

代码解析能力按语言和关系种类声明，不把语法解析成功等同于引用解析完整。构建报告明确已处理范围、
未解析引用及预算中断；消费者不能把局部图中的“没有结果”解释为仓库中不存在关联。

构图预算限制扩展范围与规模；查询预算限制执行时间、路径深度、结果规模及内存使用。
LIMIT 不是遍历工作量的完整边界。预算或取消造成的失败必须可辨识，不能伪装成完整的空结果。

### 语言能力与共同内核

Graph、Node、Relation、快照、预算和 Cypher 不依赖具体语言。文件识别复用 gotreesitter registry，
通用适配器将 outline 转为具体类别和词法 contains；没有固定的语言白名单。新 grammar 可使用上游
注册机制接入，AST 在事实提取后释放，不进入公开模型。未知声明类别保持诊断，不引入 Symbol 兜底分类。

语法可用性、声明提取和引用解析是不同层次。Languages 只枚举注册项；Capabilities 按语言声明类别、
关系及限制。只具备 outline 的语言报告 unsupported_resolution，保留有用声明但不伪装完整图。
Go 使用包和接收者规则；Python 与 JS/TS 使用模块路径候选及局部绑定规则。规则不能凭同名跨语言绑定。

模块 import 扩展与关系解析共享路径候选函数，受同一范围及预算限制，不扫描全仓或安装依赖。
相对路径有多个匹配时保留候选；Python 绝对 import 缺少运行时搜索路径证据，即使只有一个本地目标，
仍不提升为 exact。函数调用只解析未遮蔽的同文件模块级函数；导入调用和动态分派保持未解析。
语言专有路径配置、包初始化及导出绑定不由通用同名搜索替代。

### 消费者各自拥有策略

- CCR 根据 diff 定位入口，再查询声明、调用关系、marker 等证据，决定 review unit 的拆分、
  context 选择与 token 预算。CodeGraph 不内置 review unit 或评审策略。
- repocli 决定候选测试范围，补充候选及其相关文件，再通过图查询选择关联测试。
  测试范围、选择保守性和不确定时的回退由 repocli 决定。

两者共用代码事实与图查询，不要求采用相同构图范围、遍历方向或消费流程。

## 能力与验证边界

图引擎锁定为 GoGraph v0.15.0，源码解析依赖 gotreesitter v0.52.0。
当前 Go 适配器提取函数、方法、结构体、接口、字段、其他命名类型、类型别名、单名称变量和常量及声明注释，解析同包函数
及模块内 import 函数调用。只提取命名 struct/interface 字面量的直接成员，不展开匿名嵌套类型、接口
嵌入产生的继承方法或提升成员。Capabilities 的 Declarations 使用公共 NodeKind 声明实际提取种类。
接收者、回调和闭包体中的调用保留未解析诊断；第三方模块、build tags 和类型检查不在当前能力范围。
Python、JavaScript、TypeScript、TSX 提取 outline 声明、局部函数调用、源码 import 和前置注释 marker。
其他注册语言使用通用 outline 并报告引用解析未覆盖；Java、Rust、C/C++、Ruby 有真实源码声明契约测试。
references / extends / implements 自动提取尚未实现，枚举声明不表示具备提取能力。

契约测试覆盖：

- 多段关系、反向调用、平行边独立属性、自递归与有界循环路径。
- 声明 marker 列表查询、测试入口可达性、整条路径的 confidence 过滤及调用位置返回。
- 具体标签与 kind 一致、字段/接口方法独立身份、跨文件接收者归属及其歧义和预算约束。
- Go API 建图接 Cypher 查询、只读拒绝写入、预取消请求及结果行数上限。
- 跨文件真实语法提取、重复构建幂等、补充文件后重解析、源码快照冲突和预算回滚。
- Document 与文件输入的混合语言等价性、跨入口身份与关系解析、输入字节所有权及失败批次回滚。
- 无 grammar Document 的 File 节点入库、零声明、unsupported_language 诊断，及与解析文件一致的
  幂等、内容冲突、所有权与预算契约。
- Extract 事实投影的内容与位置、不发布图状态、缓存单次解析、内容身份失配重解析、
  已加载 Document 免解析投影及并发安全；import 名称（具名/整模块/通配）与 export 别名映射。
- Python 语句级事实的种类、顺序、表达式词汇、import 挂靠与绑定名；Go `//go:embed` 诊断
  在 Extract 与构建报告中的一致性。
- marker 内容及位置往返、普通查询结果可独立修改、并发查询与批次发布。
- 混合语言隔离、第三方 grammar 注册、模块候选、局部遮蔽、缺失能力诊断与多语言预算回滚。

测试不构成 CCR/repocli 迁移已完成的证明。大仓性能、增量重建成本与消费者接入回归仍需验证。

GoGraph 当前模块要求 Go 1.26；消费者接入前需统一工具链并锁定验证过的依赖版本。
Cypher 支持范围以所锁定版本及契约测试为准，不承诺完整 Neo4j 兼容。
