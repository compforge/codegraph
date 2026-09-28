# Semantic contracts 0.8 评测记录

本轮将 Scope/Binding、Relation/Evidence、材料提取和评测身份统一。旧图保留原始产物，
使用当前 oracle 显式重评后，与新实现比较；六仓 InputIdentity、EvaluatorIdentity 相同，回归门禁全部通过。

## 目标命中

| 仓库 | 引用：旧 → 新 / 分母 | 调用：旧 → 新 / 分母 | 错误 exact |
|---|---:|---:|---:|
| go-stdx | 125 → 125 / 125 | 16 → 16 / 16 | 0 |
| agentgo | 9656 → 9656 / 9689 | 1152 → 1152 / 1262 | 0 |
| repocli | 2885 → 2885 / 2959 | 340 → 340 / 363 | 0 |
| python-stdx | 7 → 7 / 11 | 4 → 4 / 4 | 0 |
| agentue | 5 → 5 / 8 | 3 → 3 / 4 | 0 |
| doctor | 10653 → 31171 / 33188 | 3656 → 3659 / 5288 | 0 |

## 新评分范围

六仓组织身份全部命中，declares 与 contains 的期望关系全部命中。
Doctor 的源码模块 imports 为 2369/2532；agentgo 仍有一个额外声明及其组织边，
另有两条引用的来源 occurrence 与独立 oracle 不一致。这些差异在重评旧图时也存在，保留差异，未作为本轮提升。

Python 调用与引用的目标分母仍仅覆盖人工审定项；动态分派与第三方依赖继续单列。
完整 occurrence 命中与仅目标命中是不同指标，不互相替代。

## 验证

- `make lint test build` 通过，Go 含 race detector。
- TypeScript oracle 7 个契约、Python oracle 6 个契约通过。
- 六个真实仓库同 evaluator 基线比较通过。
- 新契约覆盖 catch/for/block/var、解构属性、接收者遮蔽、多 kind/多位置、证据合并、深拷贝、独立预算、组织评分破坏性验证和旧产物重评。

这些结果描述固定输入与已声明 oracle 的测量，不证明整个语言或仓库已被完备解析。
