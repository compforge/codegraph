# 资源引用

CodeGraph 使用已有的 `references` 表达源码与文件 Document 的关联，不增加读取或写入关系。
路径表达式说明“引用哪个文件”；它被怎样使用、是否执行、文件是否存在于运行环境，都不是这条边的结论。

## 职责与启用

- ECMAScript 适配器提取调用/构造表达式、参数原文与浅层常量、词法绑定和 `import.meta.url` 语法事实。
- 核心根据显式 `ResourceReferenceRule` 建立引用、补料绑定、证据与缺口，执行现有预算和准入策略。
- `semantics/nodejs` 等独立包声明平台 API 的语义；调用方也可以为自己的库声明规则。
- 调用方选择规则和输入材料；CodeGraph 不读取宿主文件、不执行代码、不下载依赖。

`Options.ResourceReferences` 默认为空。Node.js 适配包由调用方显式导入：

```go
opts := codegraph.Options{ResourceReferences: nodejs.ResourceReferences()}
```

Node.js 规则声明全局 `URL`、`node:url` / `url` 导出的 `URL` 构造器。
`new URL("./template.html", import.meta.url)` 可形成文件引用，不依赖外层函数名称，
也不要求外层存在调用。遮蔽后的 URL、自定义同名类、仅用于类型的导入不会套用这些规则。

## 自定义 API

例如某个应用声明 `asset-kit.locate(options, path, base)` 生成基于模块 URL 的文件位置：

```go
opts := codegraph.Options{
    ResourceReferences: []codegraph.ResourceReferenceRule{{
        Callable: codegraph.Callable{Module: "asset-kit", Name: "locate"},
        PathArgument: 1,
        BaseArgument: 2,
        Basis: "asset_kit_path",
    }},
}
```

规则匹配导入的真实名称，别名和 namespace/default 成员访问遵守词法绑定。
`Module` 为空表示未被词法绑定遮蔽的全局名称；`Constructor` 区分构造表达式与普通调用。
同一目标不能重复配置，参数索引必须非负且不同，`Basis` 必须提供可追溯的语义依据。
规则声明的是 API 契约，不是自动证明任意函数实现；错误配置可能产生错误关联。

当前路径原语支持无转义单/双引号相对路径，以 `import.meta.url` 为基准，在快照内规范化 `.` / `..`。
不模拟运行时重赋值或 API 修改。没有默认工作目录；不处理绝对 URL、目录 URL、URL 编码/查询/片段、JS 转义、模板插值或任意值传播。
影响参数位置的 spread 保留为未知。未知表达式不生成猜测的目标边。

## 图与生命周期

完整图发布 `Reference(referenceKind=references)` 和 `Resource` 元数据：

- `Expression`：路径参数原文；spread 使参数位置未知时保留整个调用原文。
- `Path`：已解析的快照相对路径；无法确定时为空。

目标已提供时，引用位置和所属声明/Document 均通过 `references` 连接目标 Document，
共享语义规则的 `Basis` 和源码位置。发生位置独立于目标绑定，补料不会改变其身份。
目标尚未提供产生 `resource_not_loaded`；路径未知产生 `unresolved_resource`。
不存在的目标不会被虚构；后续 `AddDocuments` / `Wait` 绑定同一个引用，旧图保持不变。

`AnalysisAdmission` 不发布 Reference，但保留声明到文件的关系及必要的局部变量桥接。
是否沿引用传播影响、如何选择测试，由 repocli 等消费者决定。

## 提取与构建隔离

`Facts.Invocations` 是生产侧的通用调用参数/绑定视图，与库规则无关；它不表示资源访问。
派生资源引用只存在于单次构建，不写回 Facts 或 ExtractionCache。
同一份提取结果可以用于不同语义配置，规则在 Builder 创建时复制，不能通过修改原切片改变图。
以前的 `Facts.ResourceReads`、`Reads` / `ReadReference` 已移除；消费者通过图查询资源引用，
生产侧通过 Invocations 查看通用参数事实。

验证覆盖自定义 API 与非固定参数位置、Node.js 显式启用、别名/遮蔽、未解析目标和补料、
规则与缓存隔离、精简图桥接、查询元数据及分析/发布预算。
