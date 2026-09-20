# CodeGraph

[English](README.md) | 简体中文

面向 Go 程序内嵌使用的代码属性图库，供代码评审、影响分析等工具查询关联文件、符号及关系证据。

CodeGraph 使用 gotreesitter 解析源码，以 GoGraph 承载内存属性图与 Cypher 查询。节点使用 File、
Struct、Interface、Field、Method、Function 等具体类别，关系包括调用、导入和包含等；
声明节点保存 spec、case、rule、link、doc 等结构化意图标记。

直接依赖 gotreesitter `v0.52.0`、GoGraph `v0.15.0`，要求 Go 1.26 或更高版本。
无需独立数据库服务，无强制落盘。本仓库尚未首次发布。

## 能力与边界

- 当前解析 **Go** 的函数、方法、结构体、接口、字段、其他命名类型及类型别名，构建 contains、imports 和静态包函数 calls。
- 支持跨文件、本模块 import、递归、多调用点、按需扩展和重复添加幂等。
- 支持参数化只读 Cypher，返回 Node、Relation、Path 或普通 Go 值。
- spec、case、rule、link、doc 从声明注释中提取，保留内容与源码位置。
- 多个可能目标输出 candidate 关系；无法确定目标、回调、闭包体及接收者调用输出诊断。

这不是编译器类型检查器：不评估 build tags，不解析第三方模块，不承诺动态分派完整。
其他语言以及 references、extends、implements 的自动提取尚未实现；可通过 `Capabilities()` 查看能力。

## 节点类别

`Node.Kind` 同时是节点的 Cypher 标签：`File`、`Struct`、`Interface`、`Field`、`Method`、
`Function`、`Type` 或 `TypeAlias`。`Type` 表达 `type ID int` 等其他命名类型；`TypeAlias`
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
    os.DirFS("./repo"),
    []string{"main.go"},
    codegraph.Options{
        ModulePath:    "example.org/demo",
        ExpandImports: true,
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

也可以 `New(snapshot, options)` 创建空图，再通过 `AddFiles(ctx, source, paths...)` 补充文件。
每次补充会在后台重建当前局部图并原子替换；这是正确性优先的批次更新，不是增量图引擎优化。
查询可以继续读取上一批次。before/after 应创建不同的 Graph；同一路径重新加入不同字节会返回
`ErrSnapshotChanged`。调用者负责保证 `fs.FS` 的不可变性和文件访问边界。

## Marker 与查询属性

```go
// +spec=`调用必须保留源位置`
// +case:id=parallel,expect=`保留两个调用点`
// +rule=`不能按端点合并关系`
// +link=docs/kernel.md
// +doc=`入口函数`
func Entry() { Work(); Work() }
```

Marker 绑定到声明的文档注释；结构化 payload 原样保留，不执行表达式。

| 对象 | 常用属性 |
|---|---|
| Node | id、kind、name、qualifiedName、language、path、line、column、startByte、endByte、snapshot |
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

测试覆盖跨文件调用、别名 import、多重边、marker 往返、路径置信过滤、查询只读/预算、
批次回滚及并发查询。源码结构与设计依据见 [内核设计](docs/kernel.md)。
