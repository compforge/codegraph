# CodeGraph

[English](README.md) | 简体中文

CodeGraph 是可嵌入 Go 程序的多语言代码分析库。你提供一组源码文件，它通过静态分析构造符号及其
关系图，让代码评审、影响分析和源码导航工具能够查询代码之间的关联，以及建立这些关联的依据。
它运行在调用方进程内，无需独立数据库服务，也不要求源码落盘。

## 解决什么问题

构建代码工具时，经常需要从一个改动或符号出发，找到值得进一步检查的代码：

- **调用与引用**：谁调用了这个函数？哪些声明引用了这个类型？
- **结构与依赖**：一个类型有哪些成员？这个文件导入了哪些模块？哪些类型声明了继承或实现关系？
- **结果依据**：关联发生在哪段源码？目标是否已确定，还是只有候选？哪些位置尚未解析？

CodeGraph 负责解析和组织代码事实，提供可查询的符号、关系及其证据。调用方结合自己的规则决定
如何使用这些事实：repocli 确定改动影响和测试范围，CCR 确定 review 范围并组织评审上下文。

## 输入与结果

```text
源码文件（路径 + 内容） → 静态分析 → 符号关系图 → 查询关联与证据
```

**Document 是输入材料。** 通常由一个逻辑路径和完整源码组成，可来自内存、工作区或 Git 中的某个
版本。路径不必在磁盘存在，但同一张图的材料必须属于同一份快照。调用方负责选择并读取材料，
CodeGraph 在提供的范围内分析，不自动扫描仓库或下载依赖。

**Graph 是分析结果。** 函数、类型、字段等声明构成符号节点，调用、引用、成员归属、导入及类型关系
连接这些节点。图也保留文件和包、模块等组织信息。例如 `Entry()` 调用 `Work()`，会形成
`Entry → calls → Work`，既可以从 Entry 找被调用方，也可以从 Work 反查调用方。
通过 Go 访问器或只读 Cypher 查询，得到节点、关系及其源码位置。
消费侧视图以这些节点和关系为唯一代码事实来源。
已识别的调用和引用位置也有独立节点：即使只提供调用方文件，仍能查询使用发生在哪里；
补入依赖后，新的图结果会重新计算目标和证据，原先的候选关系也可以被修订。

**证据和诊断说明结果能支持什么判断。** 一条关系保留建立它的依据与置信等级；有些目标可以在当前
材料中确定，有些只能提出受名称、导入或类型线索约束的候选。缺少目标或能力时保留局部诊断。
因此，查到候选值得进一步核实，未查到关系也不能直接解释为仓库中没有关联。

Go、Python、JavaScript、TypeScript、TSX 提供专有的名称绑定与关系解析；其他已注册语法的语言
主要提供声明结构。不同语言和代码形态的覆盖不同，详见 [语言能力与限制](docs/language-support.md)。
CodeGraph 不执行编译器类型检查，也不保证完整的运行时调用分派分析。

## 快速开始

使用 [go.mod](go.mod) 声明的 Go 工具链，在应用中添加依赖：

```sh
go get github.com/compforge/codegraph
```

以下程序构建图并查询 `Work` 的调用方：

```go
package main

import (
    "context"
    "fmt"

    "github.com/compforge/codegraph"
)

func main() {
    ctx := context.Background()
    g, report, err := codegraph.Build(ctx, "revision-1", []codegraph.Document{
        {Path: "main.go", Content: []byte("package demo\nfunc Entry(){Work()}\nfunc Work(){}")},
    }, codegraph.Options{})
    if err != nil {
        panic(err)
    }
    for _, diagnostic := range report.Diagnostics {
        fmt.Printf("%s: %s\n", diagnostic.Location.Path, diagnostic.Code)
    }
    rows, err := g.Query(ctx, `
        MATCH (caller:Function)-[:calls]->(:Function {name:$name})
        RETURN caller.name AS caller`, map[string]any{"name": "Work"})
    if err != nil {
        panic(err)
    }
    for _, row := range rows {
        fmt.Println(row["caller"])
    }
}
```

输出为 `Entry`。构建成功仍可能伴随局部分析缺口，由调用方判断其与当前任务的关系。
更多可执行示例见 [example_test.go](example_test.go)。

## 深入使用

- [使用指南](docs/usage.md)：分批补充材料、复用分析结果、源码定位与更多关系查询，以及从图生成文件大纲。
- [Document 契约](docs/document.md)：材料身份、快照一致性及 Git 子模块边界。
- [内核设计](docs/kernel.md)：需要扩展或参与开发时，了解分析流程与职责划分。
