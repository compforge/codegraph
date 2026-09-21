# CodeGraph

[English](README.md) | 简体中文

用 Go 编写、可内嵌的多语言代码属性图库，供代码评审、影响分析等工具查询关联文件、符号及关系证据。

CodeGraph 使用 gotreesitter 解析源码，以 GoGraph 承载内存属性图与 Cypher 查询。节点使用 File、
Struct、Interface、Field、Method、Function 等具体类别，关系包括调用、导入和包含等；
声明节点保存 spec、case、rule、link、doc 等结构化意图标记。

直接依赖 gotreesitter `v0.52.0`、GoGraph `v0.15.0`，要求 Go 1.26 或更高版本。
无需独立数据库服务，无强制落盘。本仓库尚未首次发布。

## 能力与边界

- 当前解析 **Go** 的函数、方法、结构体、接口、字段、其他命名类型、类型别名及单名称变量和常量，构建 contains、imports 和静态包函数 calls。
- 提取 **Python、JavaScript、TypeScript、TSX** 声明、词法包含、本地源码 import、同文件中未被遮蔽的模块函数调用及声明注释 marker。
- 其他 gotreesitter 已注册语言使用通用语法/声明适配器；缺少 outline、未知声明类别和未实现的关系解析均输出明确诊断。
- 支持跨文件、本模块 import、递归、多调用点、按需扩展和重复添加幂等。
- 支持参数化只读 Cypher，返回 Node、Relation、Path 或普通 Go 值。
- spec、case、rule、link、doc 从声明注释中提取，保留内容与源码位置。
- 多个可能目标输出 candidate 关系；无法确定目标、回调、闭包体及接收者调用输出诊断。

这不是编译器类型检查器：不评估 build tags，不解析第三方模块，不承诺动态分派完整。
references、extends、implements 的自动提取尚未实现。识别到 grammar 不等于具备完整的语言语义。

`Language(path)` 识别文件语言，`Languages()` 列出注册的 grammar，`Capabilities()` 返回 Go、Python、
JS、TS、TSX 的适配能力。`Capabilities("rust", "java")` 按需查看其他语言的声明提取能力，
不会预加载所有 parser；未知语言名不返回能力声明。

Python import 使用仓库相对的模块候选，绝对 import 因运行时搜索路径未知而保留为 `candidate`。
JS/TS 解析相对源码路径及 index 文件；多个匹配文件保留为候选。不评估包元数据、tsconfig alias、
Python 包初始化、re-export 的符号绑定、跨文件函数调用和运行时分派。`Complete` 只覆盖这些声明边界内
的已提取事实，不代表与编译器或运行时等价。

## 节点类别

`Node.Kind` 同时是节点的 Cypher 标签：`File`、`Struct`、`Interface`、`Field`、`Method`、
`Function`、`Type`、`TypeAlias`、`Class`、`Variable`、`Enum` 等具体声明类别。各语言实际覆盖见
`Capabilities(language)`。`Type` 表达 `type ID int` 等其他命名类型；`TypeAlias`
表达 `type Alias = ID` 等显式别名。分类描述声明本身，不推断底层类型。symbol（符号）只是代码声明的
统称，不是图中的类别或标签。

字段和接口中显式声明的方法均为独立节点，有自己的位置和 marker。可以直接查询成员：

```cypher
MATCH (s:Struct)-[:contains]->(f:Field)
RETURN s, f
```

`contains` 记录词法归属。接收者方法还会从已加载包内的接收者类型建立 `contains` 边，支持跨文件；
关系的 `basis` 用 `declaration` 与 `receiver_declaration` 区分两种依据。接收者未解析或存在歧义时
输出诊断。不展开匿名嵌套类型或提升成员。

## 快速使用

以下片段适合放入消费者程序；完整可运行示例见 [example_test.go](example_test.go)。

```go
g, report, err := codegraph.Build(
    ctx,
    "revision-1", // 调用方标识不可变源码快照
    []codegraph.Document{
        {Path: "main.go", Content: []byte("package demo\nfunc Entry(){ Work() }")},
        {Path: "work.go", Content: []byte("package demo\nfunc Work(){}")},
    },
    codegraph.Options{
        ModulePath: "example.org/demo",
    },
)
if err != nil {
    return err
}
// report.Complete 只描述本次已请求/扩展范围，不代表整个仓库。
// 消费者必须检查 report.Diagnostics，再决定是否回退到其他分析方式。
if !report.Complete {
    return fmt.Errorf("partial code graph: %v", report.Diagnostics)
}

rows, err := g.Query(ctx, `
    MATCH p=(caller:Function)-[:calls*1..3]->(target:Function {name:$name})
    WHERE all(r IN relationships(p) WHERE r.confidence = 'exact')
    RETURN caller, p`, map[string]any{"name": "Work"})
if err != nil {
    return err
}
for _, row := range rows {
    caller := row["caller"].(codegraph.Node)
    path := row["p"].(codegraph.Path)
    // caller、path 包含脱离底层引擎的值，可直接交给业务消费者。
    _, _ = caller, path
}
```

也可以 `New(snapshot, options)` 创建空图，再通过 `AddDocuments(ctx, documents...)` 补充源码材料。
每次补充会在后台重建当前局部图并原子替换；这是正确性优先的批次更新，不是增量图引擎优化。
查询可以继续读取上一批次。before/after 应创建不同的 Graph；同一路径重新加入不同字节会返回
`ErrSnapshotChanged`。调用方负责选择 Document 以及源码访问边界。

同一 `Build` / `AddDocuments` 可接收 `[]codegraph.Document{{Path: "server.go"}, {Path: "worker.py"}, {Path: "web/app.ts"}}` 等混合语言材料。
不同语言的同名声明不会互相绑定。`ModulePath` 只控制 Go 模块 import；所有语言共用范围、预算及原子发布规则。

### 源码 Document

`Document` 是一份构图源码输入：逻辑路径 `Path` 及其完整内容 `Content`。路径不必实际存在于磁盘，
用于快照内的源码身份、语言识别、相对 import 上下文与源码位置。Document 是输入材料，不是 `Node.Kind`。

源码来自内存或 Git revision 时，可以直接传入：

```go
g, err := codegraph.New("revision-1", codegraph.Options{})
if err != nil {
    return err
}
report, err := g.AddDocuments(ctx,
    codegraph.Document{Path: "main.go", Content: []byte("package demo\nfunc Entry(){ Work() }")},
    codegraph.Document{Path: "work.go", Content: []byte("package demo\nfunc Work(){}")},
)
if err != nil {
    return err
}
if !report.Complete {
    return fmt.Errorf("partial code graph: %v", report.Diagnostics)
}
```

- 路径使用符合 `fs.ValidPath` 的快照相对路径，以 `/` 分隔，不是绝对路径或 URL。
- `AddDocuments` 在当前批次和已加载源码之间解析关系，不会隐式获取依赖；调用方显式提供所需材料。
- Document 批次共用范围、预算、诊断和快照身份。相同输入重复加入保持幂等；已加载路径的内容
  冲突返回 `ErrSnapshotChanged` 并回滚整个批次。
- 调用期间不要修改内容；返回后，图持有独立的源码字节。

### 定位声明

消费者可以直接按源码路径和限定名定位声明，不需要重新设计节点 ID：

```go
entries := graph.Find("src/service.py", codegraph.Function, "Service.run")
for _, entry := range entries {
    callers := graph.RelationsTo(entry.ID, codegraph.Calls)
    _ = callers
}
```

`Find` 按源码位置排序；`Node`、`RelationsFrom`、`RelationsTo` 返回脱离内部存储的值。
节点位置同时提供起止行/列，diff 消费者无需再次解析源码即可把变更范围映射到声明。
kind 或限定名为空时表示通配。

## 语言扩展

语言识别不使用 CodeGraph 固定白名单。构图前可以通过 gotreesitter 的 `grammars.Register` /
`RegisterExtension` 注册 grammar，通用适配器依据其 tags 和归属规则构建具体类别节点及 contains 关系。
AST 不穿透图 API；未知声明类别输出诊断，不降为笼统的 `Symbol` 节点。关系解析需要语言专有绑定规则；
仅支持声明提取的语言始终报告局部覆盖。可运行的[扩展测试](language_extension_test.go)展示了这一边界。

## Marker 与查询属性

```go
// +spec=`调用必须保留源位置`
// +case:id=parallel,expect=`保留两个调用点`
// +rule=`不能按端点合并关系`
// +link=docs/kernel.md
// +doc=`入口函数`
func Entry() { Work(); Work() }
```

Marker 绑定到 Go 声明文档注释，或 Python、JS/TS 声明前的 `#`、`//`、`/* */` 注释（支持 export 包装）；
结构化 payload 原样保留，不执行表达式。

| 对象 | 常用属性 |
|---|---|
| Node | id、kind、name、qualifiedName、language、path、line、column、endLine、endColumn、startByte、endByte、snapshot |
| 声明 marker | markers（种类列表）、spec/case/rule/link/doc（各自内容列表）、markerData（完整结构 JSON） |
| Relation | id、kind、source、target、confidence、basis、path、line、column、startByte、endByte |

confidence 为 `exact` 或 `candidate`，不是概率。`exact` 指已加载范围内的唯一语法绑定，
仍受声明的语言能力限制。身份使用 `n.id` / `r.id`；Cypher 的 `id(n)` 是引擎内部编号，不是源码身份。
源位置的字节区间左闭右开，行和字节列从 1 开始。

`Query` 支持实体、路径、字符串、int64、float64、bool、null、列表和 map 的结果转换。
其他 Cypher 专有值类型会返回错误。查询禁写、禁 procedure，变长路径必须有明确上界；
Options 为文件数、源码体积、节点/边数量、解析/查询超时、路径深度和结果体积提供有限默认预算。
错误时不返回部分查询行。读文件/解析失败会发布带诊断的局部图；预算、快照冲突和取消则回滚整个批次。

## 本地验证

```sh
make fmt
make lint test build
```

测试覆盖多语言声明与绑定、grammar 扩展、跨文件调用和 import、多重边、marker 往返、路径置信过滤、查询只读/预算、
批次回滚及并发查询。源码结构与设计依据见 [内核设计](docs/kernel.md)。
