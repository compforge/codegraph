# Namespace 组织复测

日期：2026-09-28；版本 0.7.10。基于主线 `d8afa119c6741e3be7b8716f219c5867087f5da9`
加本轮改动。固定仓库、源码清单、Go 编译器 / CPython 参照和人工绑定目标未变化。
工具链为 Go 1.27.1、CPython 3.14.7。

源码及版本摘要：`4f6a724a4ab22cae2fc52cd3c879ea81093951858f0f40880c84c7b5cca966b4`。
评测器摘要：`e94df34532d84d286f021f0eeb93d7259d3874f33893597342229c24467e4d1a`。

## 真实仓库结果

| 仓库 | 契约声明 | 引用目标命中 | 调用目标命中 | 新组织节点 |
|---|---:|---:|---:|---|
| go-stdx | 67/67 | 125/125 | 16/16 | 11 Package |
| agentgo | 2578/2578 | 9656/9689 | 1152/1262 | 13 Package |
| repocli | 635/635 | 2885/2959 | 340/363 | 9 Package |
| python-stdx | 422/422 | 7/11 | 4/4 | 10 Package、33 Module |
| agentue | 104/104 | 5/8 | 3/4 | 3 Package、8 Module |

逐项核对五仓 0.7.9 报告，全部 `measurements` 与 `bindings` 字段相同，包括错误 exact、
候选数量、静默缺口和未评估绑定数量。新组织节点单独记录为 `organization/Package`、
`organization/Module` 未评估项，不混入源码声明的分母或计作绑定质量提升。
组织节点与关系的独立真实仓库 ground truth 尚未建立；表中节点数量只表示产出，不表示语义准确率。

评测器为可选 Node.Location 和组织节点分类做了调整，摘要与 0.7.9 不同。因此上述是明确范围的
历史指标核对，不是通过 `CORPUS_BASELINE_DIR` 的同身份基线门禁；未修改旧报告或放宽身份校验。

## 契约验证

`namespaces_test.go` 使用明确的源码与期望关系，独立验证：

- Go 多文件包合并、不同目录同名包及外部测试包隔离；一次 import 只连接一次 Package。
- Document 的 declares 与 Package / Module / Class 的 contains 分离；跨文件接收者方法归属。
- Python 多层普通包、模块、类和方法的嵌套；再导出不改变成员归属。
- 倒序构图与分批补入父包生成相同最终图，既有组织与声明身份稳定，contains 无环。
- `.py` / `.pyi` 独立身份与导入候选；无初始化材料的目录不被假定为普通包。
- 组织节点不被当作类或函数目标，缺失依赖和解析失败不制造组织节点。
- 可选位置的 JSON/Cypher 投影、返回位置副本、节点/关系预算及取消回滚。

`make fix lint test build test-corpus` 全部通过，包含根模块与评测模块的 race 测试。
原始证据位于 `.corpus-results/<repo>/`；模型与语言边界见[命名空间组织](../../docs/namespaces.md)。

## 消费方接入

- 源码归属查询使用 `Document -[:declares]-> node`；语义成员使用 `contains`。
- Go import 目标为 Package，Python import 目标为 Module / Package；JS/TS 保留 Document 目标。
- `Node.Location` 为可选指针，处理组织节点时沿 declares 获取源码贡献。
- Namespace 与 Symbol 保持逻辑术语，不增加统一上位标签或互斥类别字段。

这些是公共模型的契约调整；本轮未修改或验证 repocli 等消费方接入代码。
