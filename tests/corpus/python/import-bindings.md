# Python 导入路径绑定复测

日期：2026-09-28；版本 0.7.8。基于主线 `da6315389bca1828749b578a51448a36139359b3`
加本轮改动。固定仓库、人工审定目标及 CPython 参照与[首轮测量](first-run.md)相同。
工具链为 Go 1.27.1、CPython 3.14.7。

源码及版本摘要：`9b1a797ceaf58d077962a80e3b781f4fc03ef441763918c1678388ccbb7e5ce4`。
评测器摘要保持为 `e1dde0e53da1cc103dba41dd87378b4e8f70222e2ce60ecb86df04acc54b66a0`。

## 结果

| 仓库 | 审定引用目标命中 | 审定调用目标命中 | 错误 exact | 引用静默缺口 |
|---|---:|---:|---:|---:|
| python-stdx | 3 → 5 / 11 | 3 → 4 / 4 | 0 | 2（不变） |
| agentue | 0 → 2 / 8 | 1 → 3 / 4 | 0 | 3（不变） |

新增四个正确引用目标与三个正确调用目标；评分范围内额外候选仍为 0。
两仓库的声明、引用位置、调用表达式和 import 事实计数全部不变。
未评分引用边由 109/56 增至 211/130，未评分调用边由 273/97 增至 303/132；
这些边不能计为已验证正确。目标命中仍只覆盖人工审定样本。

已核对的新增目标为：

- python-stdx：connector 中的 `RedisClient` 与 `create_backend` 引用，以及 `create_backend(...)` 调用。
- agentue：runner 中的 `PatchEmitter` 与 `extract_patch_op` 引用，以及 `extract_patch_op(...)` 和
  emitter 中的 `event.to_json()` 调用。

## 原因与修复边界

原实现仅在快照根目录寻找 Python 绝对导入，无法匹配 `src/python_stdx` 与
`sdks/python/src/agentue` 下的当前包。路径候选现在也利用导入首段与当前文件同名祖先目录的对应关系，
复用现有导入、转导出、引用和调用绑定流程。

所有绝对导入仍为 candidate；多个根同时匹配时保留全部候选。不会扫描其他目录按符号同名绑定，
不会猜测不在当前包路径中的外部包根，也不执行 Python 初始化或推断运行时 sys.path。
相对导入保持原有路径与置信规则。

注解中的引用、实例方法引用与调用的一致性、f-string 读取事实等首轮缺口仍需后续处理。

## 验证与旧基线审阅

- `make fix lint test build test-corpus` 通过，包含 Go race 测试与全部五个真实仓库。
- 新增回归覆盖平铺 / 源码子目录 / 嵌套 SDK 布局、别名、参数遮蔽、转导出、补料后解析、
  多根歧义、无关根排除、类型桩、相对导入及关系预算原子失败；修复前可复现路径缺口。
- 对旧报告执行显式基线比较：三个 Go 仓库通过；两个 Python 仓库仅触发
  `references: candidate expansion needs review`，因此该比较命令返回非零。

该提示来自引用候选最大数量由 0 增至 1。已检查最终图：引用候选集合均不超过一个目标，
新增审定引用全部匹配人工目标，调用候选最大数量保持为 3/1。
这属于需要审阅的新增单候选证据；原门禁、人工清单和旧报告均未修改，不能把本轮描述为旧基线全通过。
未审定绑定继续保持未评估，门禁提示与上述审阅证据一同保留。

当前测量位于 `.corpus-results/<repo>/`，旧基线比较位于 `.corpus-results/baseline-check/<repo>/`。
复现比较时使用上一轮报告目录：

```sh
make test-corpus \
  CORPUS_BASELINE_DIR=<首轮报告目录> \
  CORPUS_REPORT_DIR=<比较报告目录>
```
