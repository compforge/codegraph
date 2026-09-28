# Confidence tiers 0.9 评测记录

将 confidence 从 exact/candidate 调整为 exact、scoped、name_only、heuristic。
对 0.8 保留的六仓图显式重评，再用相同 InputIdentity 与 EvaluatorIdentity 比较当前实现。
历史 candidate 保持未分级，不按 Basis 补造旧证据。新版本使用其实际静态约束重新给出精度。

## 拓扑与命中

六仓节点内容、关系身份、端点、种类和 occurrence 全部保持一致，没有新增或丢失关系。
原有 Evidence 的 Basis 与支撑位置集合也保持一致；confidence 的重新分级单独计量。

| 仓库 | 引用命中 / 分母（前后相同） | 调用命中 / 分母（前后相同） | 错误 exact |
|---|---:|---:|---:|
| go-stdx | 125 / 125 | 16 / 16 | 0 |
| agentgo | 9656 / 9689 | 1152 / 1262 | 0 |
| repocli | 2885 / 2959 | 340 / 363 | 0 |
| python-stdx | 7 / 11 | 4 / 4 | 0 |
| agentue | 5 / 8 | 3 / 4 | 0 |
| doctor | 31171 / 33188 | 3659 / 5288 | 0 |

## 分级变化

以下覆盖全部关系种类，包含组织、导入、调用、引用和类型关系。所有旧 exact 均保持 exact；
其余三列分别来自旧 candidate 的重新分析，没有将分级变化当成召回提升。

| 仓库 | exact → exact | candidate → scoped | candidate → name_only | candidate → heuristic |
|---|---:|---:|---:|---:|
| go-stdx | 225 | 63 | 0 | 0 |
| agentgo | 8975 | 7208 | 109 | 22 |
| repocli | 2161 | 2416 | 4 | 0 |
| python-stdx | 1109 | 619 | 115 | 0 |
| agentue | 311 | 276 | 20 | 0 |
| doctor | 62347 | 295 | 47 | 0 |

heuristic 在本轮语料中来自 Go 方法名集合的 implements 推测，其签名与完整方法集尚未验证。
每档的命中、其他目标和未评估计数保存在 report.json 的 bindings.byConfidence 中；
maxTargets 独立统计每个 occurrence 的目标集合规模，不因分级拆分而缩小。

## 验证与复现

- `make lint test build` 通过，包含 Go race detector、TypeScript 7 项及 Python 6 项 oracle 契约。
- 六仓基线门禁通过。具体输入 commit、源码摘要、工具链和评分器身份保存在 report.json。
- 对照 graph.json 的节点内容及 `(ID, Source, Target, Kind, Location)`，再统计同 ID 关系的 confidence 变化。
- 新契约测试覆盖四档排序、独立证据取最高档、必要推导链取最低档、输入顺序无关、
  弱证据不投票升级、低档备选不产生 exact 冲突、查询往返、目标集合独立计量和历史重评来源。

复用保留的 0.8 图时，先以 `CORPUS_REEVALUATE_DIR` 显式重评，再以该输出作为
`CORPUS_BASELINE_DIR` 执行当前实现；具体命令见 docs/corpus.md。

Python 目标分母仍仅覆盖人工审定绑定；动态分派、外部依赖和未审定目标保持未评估。
既有静默缺口及 oracle 差异继续保留。这次验证说明固定输入下拓扑没有退化，不能证明语言解析完备，
也不把 confidence 当成经过校准的正确率或业务影响概率。
