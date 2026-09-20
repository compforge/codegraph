# AGENTS.md

## 项目定位与边界

CodeGraph 是可内嵌的代码属性图 Go 库，直接依赖 gotreesitter 与 GoGraph。
根包提供公共 API；当前语言适配器支持 Go，具体覆盖以 `Capabilities()` 与契约测试为准。

## 代码地图与核心模块

```text
VERSION                                # 项目版本
graph.go、node.go、relation.go、marker.go  # 公共图模型与能力声明
build.go、build_graph.go                 # 范围构建、诊断及原子发布
query.go                                # 只读查询及领域结果还原
internal/
  extract/                              # gotreesitter 事实与 Go 词法补充
  resolve/                              # 范围内 import、调用目标与置信依据
  graphstore/                           # GoGraph、查询限制及引擎值转换
graph_test.go、example_test.go           # 契约测试与可执行示例
docs/design.md                          # 稳定模型、主流程与设计依据
```

## 关键约定

1. 核心模型沿用 Graph、Node、Relation；Node.Kind 为 File / Symbol，代码语义表现为属性与关系类型。
2. spec、case、rule、link、doc 属于核心 marker 类型；置信依据属于关系属性。
3. CCR 的评审策略与 repocli 的测试选择策略留在消费方；本库负责代码事实和通用图查询。
4. AST 与图引擎内部类型不穿透公共 API；局部分析与未解析引用必须保留可辨识的覆盖信息。
5. 同一 Graph 只容纳同一源码快照；修改批次必须完成构建后原子发布。新增语言先声明能力并补契约测试。
6. 验证入口为 `make lint test build`，测试启用 race detector。
7. 根目录 `VERSION` 记录项目版本，格式为 `X.Y.Z`。任何代码文件变更（含测试代码、增删及重命名）必须在同一提交同步 bump `VERSION`，默认递增 patch；纯文档变更无需 bump。

## References

- [设计方案](docs/design.md) — 模型、构建与查询流程、依赖边界和验证状态。
