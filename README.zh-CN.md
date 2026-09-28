# CodeGraph

[English](README.md) | 简体中文

用 Go 编写、可内嵌的多语言代码属性图库，供代码评审、影响分析等工具查询关联文件、声明及关系证据。
运行在调用方进程内，无需独立数据库服务，也不要求落盘。

## 核心能力

- 从源码提取声明、导入、引用、调用、类型关系及意图标记。
- 为 Go、Python、JavaScript、TypeScript、TSX 提供语言专有绑定规则；其他注册 grammar 的语言使用通用声明提取。
- 连接所提供文件中的事实，保留递归、多次调用位置及关系依据。
- 接受内存或调用方准备的仓库快照材料，包括不展开内部内容的 gitlink。
- 通过参数化、只读 Cypher 或 Go 访问器查询节点、关系与有界路径。
- 在可用图之外保留候选目标与局部分析缺口。

CodeGraph 提供已加载范围内的静态证据，不执行编译器类型检查，也不保证完整的运行时分派分析。
仓库发现、Git 读取、依赖获取、影响判定及测试选择由调用方负责。

## 核心概念

调用方提供 **Document**：逻辑路径及其源码内容，或者 gitlink 固定 commit。
一个 **Graph** 保存同一源码快照中的事实，可以分批补充材料。

节点使用 `Document`、`Package`、`Module`、`Class`、`Function` 等具体类别。
Symbol 表达代码声明，Namespace 表达语言如何组织成员，两种逻辑角色可以重叠：Class 既声明类型，也组织成员。

关系连接这些节点：`declares` 记录源码贡献，`contains` 记录直接成员归属，
`imports`、`references`、`calls`、`extends`、`implements` 表达代码关系。
每条关系保留发生位置及证据，confidence 为 `exact` 或 `candidate`，不表示修改传播的概率。
找不到目标时保留诊断，不补造节点。

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

分批构建与更多查询用法见 [使用指南](docs/usage.md)。
