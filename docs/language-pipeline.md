# 语言构建流程

## 模型与职责

CodeGraph 从同一快照的 Document 提取事实，由语言规则建立组织与绑定，再发布带源码证据的图。
核心概念与发布不变量见 [内核设计](kernel.md)，语言覆盖见 [语言能力](language-support.md)。

公共分析层以 Entity 统一登记 Document、源码声明及合成实体，并使用同一条物化路径。
Ref 表达身份来源，与实体是否组织成员无关。Namespace 是同一 Entity 的成员组织视图，
Package/Module、显式 Namespace 和 Class 通过相同视图查成员与源码贡献；跨文件接收者归属
在绑定后补入。继承查找在直接成员之上遍历已绑定的 extends。
词法作用域、名称遮蔽和导出规则由语言实现解释，不由 contains 自动推导。

## 阶段与数据流

| 阶段 | Core 责任 | 语言接口与产物 |
|---|---|---|
| Extract | 有界调度、parser/AST 生命周期、取消 | Extractor 返回脱离 AST 的单 Document 事实 |
| Organize | 登记全部实体后统一建立成员索引 | Organizer 返回实体、Document 的语义根及关系证据 |
| Bind | 汇总绑定结果，补齐成员和类型关系 | Binder 返回导入、类型、接收者关系及本轮 Resolver |
| Resolve | 汇总关系证据和局部缺口 | RelationResolver 使用完成绑定的索引解析引用与调用 |
| Select | entitySelector 按准入策略选择内部实体与 bridges | 由 Core 执行 |
| Assemble / Publish | graphAssembler 按需生成公共值并检查发布预算，Builder 原子发布 | 由 Core 执行 |

阶段之间有显式屏障：所有语言完成 Organize 后才开始 Bind，所有 Bind 完成后才开始 Resolve。
这使晚加入的基类、接收者类型以及混合 JS/TS 文件能够参与同一轮关系解析。
语言内部可进行有界类型传播；它不能提前发布结果或自行启动另一轮构建。

语言实现只消费调用方提供的源码材料和当前批次索引，不读取仓库、安装依赖或操作图存储。
Binder 与 Resolver 将输入索引视为只读，其关系结果由 Core 登记；失败或取消返回错误，整个批次不发布。
候选目标保留 confidence 与 basis，已识别的遮蔽阻止同名回退，未解析引用保留局部诊断。

## 共享索引

`BuildScope` 是本轮材料集合；`Scope` 是源码可见范围，`Binding` 是该范围的名称绑定。
`BindResult` 返回关系贡献、诊断及本轮 Resolver。
Python/ECMAScript 的 dialect 在单文件提取中登记脱离 AST 的 SymbolTable，后续引用、调用和
接收者类型线索共享其查找结果；语言实现负责声明位置、父级跳转、参数和块等规则。

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
仅注册 grammar 的语言继续使用通用 outline，报告关系解析覆盖缺口。

Go、Python、ECMAScript 各自拥有语义规则；共享算法由它们显式调用：

- gotreesitter 的 FactProgram 提供声明、结构化签名和调用参数；CodeGraph 将事实投影为图模型，负责词法绑定、类型与关系证据。上游已支持的语法形状不另行实现提取器。
- 上游默认规则缺少的局部声明范围，通过 FactProgram 的补充规则表达；函数类型、未绑定函数字面量、完整声明头和文档归属等缺口由适配器保留有界补充。
- 通用 outline 与 marker 提取保持一个实现。
- Python 和 JS/TS 共享模块语法遍历，通过 dialect 接口提供词法登记、参数表达式、接收者和基类等差异。
- 模块绑定共享转导出与候选汇总算法，路径、兼容语言和导出策略由各自实现提供。
- Go 入口识别复用 gotreesitter 的已有语法树，产生脱离树的声明标记；消费侧只读取图中的标记。
- Go 的类型形状与传播证据位于 Go 模块，通过强类型的 Extension 携带，不进入公共 Node。

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

## 编译式构图更新方案

以下说明编译式构图的内部职责与数据交接。外部提交与等待生命周期保持不变；
分析和发布预算的版本迁移见 [使用指南](usage.md#v016-构建预算迁移)。
图粒度及预算的整体决策见 [准入更新方案](admission.md#图生成与预算更新方案)。
所有对象遵守 [上游复用边界](admission.md#上游复用是前置约束)：gotreesitter 提取事实，GoGraph
graph / Cypher 承担图引擎能力；本方案只组织 CodeGraph 必需的语义衔接、选择与发布职责。

### 参考 CPython 的编译过程

源码参照固定为 CPython `3fc4d1f9b38e6a5fbb9daa5296d04e3247783af4`，不是对最新主线的声明。
其[编译入口](https://github.com/python/cpython/blob/3fc4d1f9b38e6a5fbb9daa5296d04e3247783af4/Python/compile.c#L1683)
经 compiler_setup、compiler_mod 完成分析、生成与装配，大致流程为：

```text
source → AST / 预处理 → 符号表分析
  → 指令序列 → 控制流图及优化 → 装配 → CodeObject
```

可以直接借鉴的设计点：

- [符号表的两遍分析](https://github.com/python/cpython/blob/3fc4d1f9b38e6a5fbb9daa5296d04e3247783af4/Python/symtable.c#L635)：先收集定义、参数和使用等事实，再结合嵌套作用域判断 local / global / free / cell。不能在遇到一个名字时就忽略作用域和后续材料，直接决定它的最终含义。
- [compiler 与 compiler_unit](https://github.com/python/cpython/blob/3fc4d1f9b38e6a5fbb9daa5296d04e3247783af4/Python/compile.c#L56)：编译级状态和当前作用域的状态各有归属。符号表、常量缓存、指令序列等辅助结构服务编译，不都是最终产物。
- [指令序列与临时标签](https://github.com/python/cpython/blob/3fc4d1f9b38e6a5fbb9daa5296d04e3247783af4/Include/internal/pycore_instruction_sequence.h#L21)：源码位置贯穿中间表示，临时 label / offset 在装配时转换；内部标识与最终编码分开。
- [优化及装配](https://github.com/python/cpython/blob/3fc4d1f9b38e6a5fbb9daa5296d04e3247783af4/Python/compile.c#L1616)：指令序列先转为 CFG，再生成优化后的序列和 CodeObject；临时序列及 CFG 在成功或失败路径上统一清理。
- [CodeObject 与 FunctionObject](https://github.com/python/cpython/blob/3fc4d1f9b38e6a5fbb9daa5296d04e3247783af4/Include/cpython/funcobject.h#L25)：前者是编译产物，后者由执行 def 创建并引用前者。这里 Graph 类比 CodeObject，不把运行时调用栈、对象堆或执行循环引入 CodeGraph。

CPython 的符号表分析主要处理编译单元内的名字作用域，不替我们完成仓库级跨文件解析。
本库仍由语言适配器解释模块、类型和引用，保留静态分析的多候选、证据精度与缺口。
不为对应 CPython 阶段而新增无实际用途的指令序列、CFG 或优化器；借鉴的是分阶段转换及数据所有权。

### 内部表示与所有权

| 内部结构 | 责任及生命周期 | 与公开图的关系 |
| --- | --- | --- |
| Facts / SymbolTable | 单文件提取结果，脱离 AST，只读，可由有界缓存复用 | 保留必要语义事实，不承诺每条事实都是节点 |
| BuildScope 与本轮工作状态 | 固定本轮材料、解析上下文、取消与预算；由 Builder 管理 | 不暴露新的公共编译器对象 |
| Index：Entity / Ref、成员索引、Edge / Evidence | 单次构建的语义中间表示，Organize / Bind / Resolve 分阶段填充 | 使用内部 Ref 表达身份，尚未分配公开 Node / Relation 集合 |
| 引用绑定记录 | 使用位置、角色、所属实体及带证据的目标候选；归属本轮分析 | 生成关系或可选 Reference 节点的依据 |
| 选择结果 | 已选实体与选择理由（core、bridge、context）；只在本轮使用 | 决定哪些实体需要物化，不作为新的 NodeKind |
| 公开值生成与发布缓冲 | Ref 到公开 ID 的映射、Node / Relation 和报告；由本轮 graphAssembler 独占并交还 Builder | 校验完成后交给只读 Graph，失败则丢弃 |

Index 是已有内部分析模型的继续演进，不另建与其竞争的通用 IR。SymbolTable 拥有词法 Scope / Binding，
Entity / Edge 保存本轮语义结果；不能要求“为了成为内部 Entity，必须最终成为 Node”。
引用绑定记录可由既有 Facts 和带位置的 Edge 提供，只有缺少明确的使用到候选映射时才补充轻量索引，
不复制完整语义结果，也不通过已物化的公共 Reference 节点反查解析结果。

本轮临时编号或数组索引只在分析过程内有效；公开 ID 继续由既有源码身份规则生成，不能依赖
任务完成顺序、内存地址或遍历先后。同一输入与策略必须生成稳定的身份、证据及报告。

### 用对象承接工作与状态

阶段描述执行顺序，对象负责保存状态和维护不变量。CPython 使用 C struct 与配套函数表达这些边界：
`compiler` 持有一次编译的全局状态，`compiler_unit` 持有当前代码块的生成状态；
[symtable / PySTEntryObject](https://github.com/python/cpython/blob/3fc4d1f9b38e6a5fbb9daa5296d04e3247783af4/Include/internal/pycore_symtable.h#L75)
管理作用域及名字信息；[cfg_builder](https://github.com/python/cpython/blob/3fc4d1f9b38e6a5fbb9daa5296d04e3247783af4/Python/flowgraph.c#L80)
拥有基本块、当前构建位置和标签；[assembler](https://github.com/python/cpython/blob/3fc4d1f9b38e6a5fbb9daa5296d04e3247783af4/Python/assemble.c#L50)
拥有字节码、异常表和位置表的输出缓冲。各对象的操作围绕自己的状态展开。

CodeGraph 使用以下内部具体类型承接工作：

| 对象及归属 | 独占或管理的状态 | 主要操作与完成条件 |
| --- | --- | --- |
| `compilation`，根包，由 Builder 为每次构建创建 | 本轮固定材料、配置、阶段结果和诊断；持有各阶段对象 | `Run(ctx)` 按顺序完成分析、选择、装配，返回完整待发布结果；失败不修改 Builder.Result |
| `pipeline.Analyzer` | 语言分组、适配器、绑定结果和 Resolver、分析预算；分析期间拥有 Index 的写入权 | `Analyze(ctx, scope)` 登记实体并执行 Organize / Bind / Resolve；全部语言 Bind 后才 Resolve，成功后交还语义结果 |
| `analysis.SymbolTable` | 单文件 Scope / Binding 与按作用域组织的名字索引 | 提取时登记，完成后只读；ScopeAt / Lookup 提供词法查找，随 Facts 复用 |
| `analysis.Index`，沿用现有类型 | Entity / Ref、成员索引、Edge / Evidence 及本轮绑定结果 | 登记、合并与查询语义事实，维护身份、证据和分析增长约束；分析完成后供后续阶段只读使用 |
| `entitySelector`，根包 | AdmissionPolicy、核心实体、bridge 搜索工作状态、选择理由 | `Select(ctx)` 返回以 Ref 标识的 `selection`；搜索工作量有界，保持原始语义事实 |
| `graphAssembler`，根包 | Ref 到公开 ID 的映射、Node / Relation 缓冲、发布预算及生成统计 | `Assemble(ctx, selection, failures)` 生成公共值；统一登记身份、去重、计量和校验端点，成功后交还独立结果 |

Builder 继续拥有跨调用的材料集合、异步 session 和最后一份 Graph；`compilation` 仅覆盖一次构建。
它将原先散在 `Builder.assemble` 的单次工作状态收拢起来，不能反过来持有 Builder 并任意修改结果。
`selection` 与待发布结果是阶段间交接的值，各阶段只取得需要的输入，不共享一个任意读写的大上下文。

`Analyzer` 拥有语言分组、阶段屏障和分析状态，pipeline.Build 委托给它执行。Index 管理事实登记与索引，
Analyzer 管理执行次序，两者的职责分别可验证。分析结束后 Resolver 和阶段临时状态不交给选择或装配。
Index 的生产写入经 analysis 的登记方法计量；语言阶段交回贡献，选择与装配遵守只读契约。
内部字段不是公共 API；回归验证后续阶段不会改写语义事实或已缓存的 SymbolTable。

`entitySelector` 直接读取内部实体与关系；选择先于公共对象构造。
`graphAssembler` 收拢节点、关系、使用位置及结构上下文的生成；`publishReferences` 中的登记、
去重和预算闭包成为它的内部操作。任何 Node / Relation 写入都经过装配对象维护的登记规则，
selection 只携带选中实体及 core / bridge / context 理由。Reference 是否生成、Directory 如何补齐
由 graphAssembler 读取同一份不可变发布策略决定；策略配置由 compilation 固定，不在阶段之间修改。
实体上下文指已有语义实体的归属上下文，Directory 等路径结构由装配阶段生成并计入发布预算。
Index 只服务构建期语义登记与查找；graphAssembler 负责公共模型转换与发布检查，图存储和查询
继续走现有 graphstore / GoGraph。bridge 搜索先复用现有或上游遍历机制，再施加选择规则；
如需针对内部 Ref 保留临时工作索引，须说明接口或成本缺口，不扩展为通用图存储或查询层。

接口用于实际存在的替换边界。已有 `Extractor`、`Organizer`、`Binder`、`RelationResolver`
继续作为语言能力接口；Analyzer 通过这些接口执行多语言分析。compilation、entitySelector 和
图装配目前各有一个实现，先使用有私有状态的具体 struct 与方法。以后出现另一种生成目标时，
再由调用方按其需求提取接口；不要求每个阶段都实现同一个 Run 接口。

CPython 的 compiler_unit 是按代码块划分的编译单元，不能机械地对应 CodeGraph 的一个文件。
CodeGraph 的现有 BuildScope、词法作用域和语言分组已经表达所需边界；跨文件解析仍由同一轮
Analyzer 协调，只有出现独立的作用域工作状态时才引入对应的小对象。

对象协作可概括为以下伪代码，错误处理均逐步返回，最后才发布：

```text
Builder.Build / session
  compilation.Run(ctx)
    Analyzer.Analyze(ctx, scope)               → Index
    entitySelector（Index + policy）.Select(ctx) → selection
    graphAssembler（Index + selection + policy）
      .Assemble(ctx, selection, failures)       → Node / Relation
  Builder 将完整 draft 转为 Graph 并发布
```

### SymbolTable 与跨文件绑定

`internal/analysis/scope.go` 的 SymbolTable 管理 Scope、Binding 及名字索引，通过 AddScope、Add、
ScopeAt、Lookup 提供登记与词法查找。它沿用已有实现，保持一份名字索引。
新增语义前先检查 gotreesitter 的事实和作用域能力；能直接消费或经规则扩展得到的部分沿用上游，
只保留绑定所需的缺口与适配。

SymbolTable 回答“给定源码位置，这个名字在词法作用域里有哪些绑定”，保存单文件的作用域层次、
声明位置和目标线索。提取结束后只读，随 Facts 缓存；缓存中不写入依赖其他文件的解析结果。
Binder / Resolver 结合 SymbolTable、Index 和模块解析上下文，确定本轮跨文件、成员和类型目标，
将带证据的结果交给 Analyzer 登记。补料会重建这些结果，不修改已缓存的 SymbolTable。

语言实现负责作用域语义，包括查找父级、声明可见性、遮蔽及 Python global / nonlocal 等规则。
共享表提供登记、定位与查找机制，不能用统一的“最近同名”规则替代各语言语义。
SymbolTable 保持已支持的语言行为；针对尚未支持的语言规则记录缺口，避免借结构调整扩大语义承诺。

词法 Binding 即使没有可发布的声明目标也要保留：现有 Target=-1 表达“名字已绑定但缺少支持的
声明实体”，仍能阻止错误地向外层同名对象回退。EntitySelector 选择结果不影响符号表中的绑定，
被省略的局部实体继续参与遮蔽和目标解析。Graph 消费方只读取公开图，SymbolTable 留在生产侧。

### 各阶段的输入、产物与屏障

```text
Document → 提取 → Facts
                    ↓
本轮 Facts + ResolutionContext
  → Organize / Bind / Resolve：语义中间表示
  → Select：core + bridges + context
  → Emit：Node + Relation + 报告
  → 校验并发布 Graph
```

1. **提取**：语法层借用 parser / AST，输出声明、作用域、导入和使用位置等 Facts 后释放语法树。
   继续复用 gotreesitter，不为此次设计重写语法前端。
2. **组织**：登记本轮全部 Entity / Ref，建立语言组织、词法归属和成员索引；先登记全部实体，再连接
   组织关系，使跨文件前向引用不取决于提交顺序。
3. **绑定与解析**：所有语言完成 Bind 后再 Resolve。声明、局部变量和导入项即使最终不发布，
   仍参与遮蔽、别名、类型与调用目标分析。各阶段按现有契约交还贡献，由 Core 统一登记证据。
4. **选择**：语义结果完成后只读，依据 AdmissionPolicy 选 core、context 并搜索 dependency bridges。
   选择结果使用内部 Ref；既不删除分析事实，也不为搜索 bridge 而先创建公共节点。
5. **生成**：仅为入选实体生成 Node，并生成两端均入选的原始语义关系；Reference 等源码使用节点
   由策略单独控制。节点和关系沿用原来的身份、位置、精度及证据规则，不拼接多段边制造调用。
6. **发布**：校验端点与发布预算，完成可脱离工作状态的结果后替换 Builder.Result。Graph 可继续
   延迟建立查询索引；查询不重新运行绑定，也不回读 Facts 补充语义。

Select / Emit 分别由 Core 内部的 entitySelector / graphAssembler 承接，不增加语言插件必须实现的新接口。
已有同步 Build 与异步 session 都进入相同的构建过程，阶段边界不改变等待或取消契约。

### 一次调用如何流经各层

以 `function handle() { const local = run(); }` 为例，Facts 保留 handle、local、run 的使用位置与作用域；
绑定确定 run 的目标，语义层保留已有的 `handle ─encloses→ local ─calls→ run` 及证据。
分析策略若判定 local 连接两个核心实体，就把它选为 bridge，再生成三个节点和两条原始关系。
若完整策略还要求使用位置，则另外从调用事实生成 Reference；分析策略无需先造这个节点再删除。

这里不会因希望图更紧凑就把路径改写为 `handle calls run`。声明归属与调用的组合并不自动证明
一条新的直接调用；语义派生需要语言规则及证据，与对象生成是不同职责。

### 生命周期与预算

保留可复用 Facts 与每次重建的语义状态两种生命周期。补料后重新绑定当前材料，临时索引、选择集合
和生成缓冲由本次 Build 持有；成功后仅公开产物与必要诊断进入 Graph，失败或取消时丢弃临时状态。
Graph 不保留 AST、Resolver 或可变 Index；源位置所需信息按既有契约分离保存，不借用短命缓冲。
CPython 的清理边界可以借鉴；Go 实现不因此引入手工 arena 分配器或另一套 GC。

分析预算在内部实体、使用记录、候选、关系和证据增长时扣减；发布预算在生成公共对象时扣减。
两阶段仍各自有界。移除公共 Reference 物化只消除对象生成及索引成本，不能据此免除引用分析预算。

对象落点、实施顺序、兼容迁移和验收统一维护在 [改动计划](admission.md#实施顺序与改动范围)。
本节定义工作对象与阶段契约，预算单位和结果验证见 [分阶段预算](admission.md#分阶段预算与观测)
及 [验收要求](admission.md#验收与交付)。
