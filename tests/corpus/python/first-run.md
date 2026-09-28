# Python 固定语料首次测量

日期：2026-09-28；CodeGraph 版本 0.7.7，基于主线 `e1d0b2993a01b7a23ad4a0d548312d0b5d3a5b0c`
加本轮测试层改动。运行时解析逻辑未修改。工具链为 Go 1.27.1、CPython 3.14.7。

源码及版本摘要：`b8174b4c5f4521c1807577e7fd54ff764f890429ea392a9d5e67f2a40c4b53fd`。
评测器摘要：`e1dde0e53da1cc103dba41dd87378b4e8f70222e2ce60ecb86df04acc54b66a0`。
固定提交、压缩包校验值及源码根见 [repos.json](repos.json)。

## 事实覆盖

| 仓库 | 源码文件 | 契约声明 | 调用表达式 | import 模块位置 | 读取引用位置 |
|---|---:|---:|---:|---:|---:|
| python-stdx | 43 | 422/422 | 1182/1182 | 278/278 | 6082/6221 |
| agentue | 11 | 104/104 | 411/411 | 55/55 | 1863/1900 |

声明、调用表达式和 import 均无额外输出。读取引用另有 1,149 / 330 个 unexpected 位置；
已抽查包含参数声明、循环目标和赋值属性，不能将它们直接解释为错误绑定。
读取缺口的已核对实例包括 f-string 插值中的变量和属性，原始报告保留所有位置。

总体声明位置分母分别为 1,858 / 512，包含当前图声明契约外的参数和赋值位置；
契约声明的满额命中不表示所有源码绑定已经形成图节点。

## 审定目标样本

| 仓库 | 内部引用命中 | 内部调用命中 | 错误 exact | 引用静默缺口 | 调用静默缺口 |
|---|---:|---:|---:|---:|---:|
| python-stdx | 3/11 | 3/4 | 0 | 2 | 0 |
| agentue | 0/8 | 1/4 | 0 | 3 | 0 |

目标分母来自人工审定清单 [python-stdx.json](python-stdx.json) 和 [agentue.json](agentue.json)，
不会随 CodeGraph 输出减少。两个清单共 36 项：19 个内部引用、8 个内部调用、8 个运行时回调和
1 个外部框架调用。内部目标没有额外候选；其余目标没有全部审定，错误 exact 为 0 只适用于评分范围。

未审定的源引用分别为 6,210 / 1,892，未审定的源调用为 1,174 / 402；
图中未评分的引用边分别为 109 / 56，调用边为 273 / 97。
动态回调和外部依赖的方法不能计入已验证内部调用。

## 已核对的后续方向

1. **显式导入后的绑定**：`python_stdx.redis._connector.create_backend(...)`、
   `agentue.runner.runner.extract_patch_op(...)` 的调用目标缺失；源码具有明确 from-import。
2. **注解中的引用**：watchdog 的 `LoopStall`、connector 的 `RedisConnectionConfig`、
   emitter 的 `UIModel` 及 sse 的 `PatchEvent` 在审定位置出现静默引用缺口。
3. **实例方法引用与调用一致性**：emitter 的 `self._next_offset()` 调用命中，但对应成员引用未命中；
   `event.to_json()` 的跨模块实例类型绑定仍有缺口。
4. **读取事实提取边界**：f-string 表达式引用遗漏、参数及写入位置被记录为读取引用，需要分别核对并补最小契约用例。

这里记录观察到的缺口，未将它们归因到未经核对的 resolver 内部实现。

## 验证与复现

`make fix lint test build test-corpus` 通过：包含 5 个 Python 参照器测试、Go race 测试、
错误目标注入和分母不变契约、全部五个真实仓库。三个 Go 仓库的声明、引用目标和调用目标计数与上一轮一致。
评测器已扩展，Go 历史报告与本轮摘要不同，不作为同身份基线直接比较。

两个 Python 仓库再次运行并显式比较首轮报告，通过相同快照、工具链、评测器和输入清单的回归门禁。

```sh
make test-python-corpus
make test-python-corpus \
  CORPUS_BASELINE_DIR=<首轮报告目录> \
  CORPUS_REPORT_DIR=<复测报告目录>
```

原始产物在 `.corpus-results/python-stdx/` 与 `.corpus-results/agentue/`，复测产物在
`.corpus-results/recheck/`。评分边界与环境前提见 [Python 评测说明](../../../docs/python-corpus.md)。
