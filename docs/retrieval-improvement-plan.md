# 检索命中优化落地方案（分阶段）

## 目标

**在“全部文档”范围下提升目标主题命中率，降低答非所问，控制时延增长。**

---

## 分阶段落地顺序

### 第一阶段：参数级优化（动态 TopK + 单文档限额）

1. 动态 TopK
   - 全部文档模式：提高候选与最终返回数量。
   - 单文档模式：保持较小 TopK，保证时延。
2. 单文档限额
   - 对最终结果增加每文档最大入选条数（例如每文档最多 2 条）。
3. 参数建议（首版）
   - 全部文档：candidateTopK = 24~40，finalTopK = 8~12。
   - 单文档：candidateTopK = 8~12，finalTopK = 5~8。
4. 风险控制
   - 若时延显著上升，优先下调 candidateTopK，再观察命中率变化。

---

### 第二阶段：重排与多样性（rerank + MMR）

1. 重排（rerank）
   - 对第一阶段召回的候选进行二次评分，再截断。
   - 优先保留与问题实体强相关的片段。
2. MMR 去冗余
   - 在高相关前提下抑制相似片段，提升主题覆盖。
3. 多样性约束
   - 文档维度与主题维度同时控制，避免单主题“霸榜”。

---

### 第三阶段：兜底与监控（低置信度策略 + 可观测指标）

1. 低置信度策略
   - 条件示例：最高分低、前几名分差小、实体覆盖弱。
   - 动作示例：触发二次检索（放宽参数）或返回“证据不足”模板。
2. 可观测性建设
   - 记录检索参数、候选分布、重排前后变化、来源覆盖度。
   - 建立按知识库/问题类型分层的看板。
3. 线上保护
   - 灰度开关、阈值可配置、快速回滚。

---

## 验收指标（上线前定义）

### 指标定义

- **命中率**
  - 定义：问题实体词在返回 chunk 中出现的比例。
  - 建议统计：按日、按知识库、按问题类型。
- **首条相关率@1**
  - 定义：Top1 chunk 是否与问题主题相关（人工标注或规则评估）。
- **覆盖率@K**
  - 定义：TopK 中涉及的文档数、主题数。
- **误答率**
  - 定义：缺乏证据支撑但输出明确结论的比例。
- **时延（P95）**
  - 定义：端到端响应 P95，包含重排与兜底分支。

### 建议门槛（首版）

1. 命中率：较基线提升 ≥ 20%。
2. 首条相关率@1：较基线提升 ≥ 15%。
3. 覆盖率@K：文档覆盖与主题覆盖不低于基线。
4. 误答率：较基线下降 ≥ 30%。
5. P95 时延：增幅控制在 +15% 以内。

---

## 实施节奏

1. 第 1 周：第一阶段上线灰度，完成基线对比。
2. 第 2~3 周：第二阶段联调与离线评测。
3. 第 4 周：第三阶段上线，接入看板并完成验收。

---

## 当前状态

**已完成首版开发并通过后端测试。**

### 已实现内容（代码）

1. 动态 TopK 与单文档限额
   - 在 [`retrieveRelevantChunks()`](backend/internal/service/app_service.go:579) 引入动态参数决策。
   - 参数由 [`resolveRetrievalParams()`](backend/internal/service/app_service.go:681) 统一管理。
   - 默认参数：
     - 单文档：`candidateTopK=12`，`finalTopK=6`
     - 全文档：`candidateTopK=32`，`finalTopK=10`
     - 单文档限额：`perDocumentLimit=2`（全文档模式）

2. 重排与多样性（rerank + MMR）
   - 候选重排：[`rerankCandidates()`](backend/internal/service/app_service.go:741)
   - 多样性选择：[`selectWithMMR()`](backend/internal/service/app_service.go:777)

3. 低置信度兜底与可观测性
   - 低置信度判定：[`isLowConfidenceSelection()`](backend/internal/service/app_service.go:906)
   - 低置信度触发二次扩召回：[`retrieveRelevantChunks()`](backend/internal/service/app_service.go:599)
   - 检索日志指标：[`logRetrievalMetrics()`](backend/internal/service/app_service.go:963)

4. 测试覆盖
   - 新增检索策略测试：[`app_service_retrieval_test.go`](backend/internal/service/app_service_retrieval_test.go)
   - 已执行：`go test ./...`（backend）通过

### 后续建议

- 下一步可将阈值与权重改为可配置项（当前为常量），便于在线灰度调优。

---

## 第四阶段：AI方向高级优化

**目标**：在检索优化首版基础上，针对RAG/Embedding/LLM/Qdrant核心模块剩余痛点进行深度扩展，提升整体AI性能（命中率+20%以上、误答率-30%、时延增幅<15%）。

### 1. 当前剩余主要问题
- **召回机制单一**：Qdrant仅依赖dense向量余弦相似度搜索，无sparse/BM25融合，中文关键词、实体或长尾查询召回率低，易出现“答非所问”。
- **重排效果有限**：现有rerank仅基于关键词覆盖度评分，对复杂语义、隐含关系或长文档区分能力弱，无法有效剔除噪声。
- **切分策略导致噪声**：固定窗口切分（800字符+120重叠）易断裂语义边界，上下文填充无关片段，放大LLM幻觉。
- **查询处理不足**：无LLM辅助重写/扩展，多轮对话或模糊问题首轮召回不稳定，依赖用户精确表述。
- **缓存与上下文压缩弱**：[`embedding_cache.go`](backend/internal/service/embedding_cache.go)仅基础LRU，无语义级缓存；长上下文未压缩，LLM token浪费严重，时延易超标。
- **评估与监控缺失**：虽有[`app_service_retrieval_test.go`](backend/internal/service/app_service_retrieval_test.go)单元测试和日志，无RAG专用量化指标（faithfulness、answer relevancy、覆盖率），迭代缺乏数据驱动依据。
- **LLM生成控制弱**：低置信兜底存在，但prompt未针对本地知识库做高级优化，模型适配（尤其是中文领域）泛化不足。

### 2. 针对性解决方案（优先级排序）
1. **引入Hybrid Search**
   - **问题解决**：dense+sparse融合，显著提升关键词/实体召回率（预计25-40%）。
   - **方案路径**：Qdrant原生支持sparse向量索引与RRF融合，直接扩展现有检索管道，无需新依赖。

2. **升级语义Reranker**
   - **问题解决**：替换关键词评分，使用轻量cross-encoder模型（如bge-reranker）精选top-5，消除语义噪声。
   - **方案路径**：集成本地小模型，置于候选召回后处理，与现有MMR无缝衔接。

3. **Semantic Chunking优化**
   - **问题解决**：句子/段落边界切分+元数据嵌入，避免固定窗口断义。
   - **方案路径**：升级[`document_text.go`](backend/internal/util/document_text.go)文本处理层，保持重叠策略，同时支持标题/来源metadata注入。

4. **Query Rewrite + Multi-Query Retrieval**
   - **问题解决**：LLM轻量生成查询变体，多查询并行检索融合，提升模糊/多轮场景命中。
   - **方案路径**：在查询向量化前插入rewrite步骤，与现有动态TopK结合。

5. **Semantic Cache + Context Compression**
   - **问题解决**：重复查询命中率提升，长上下文自动摘要压缩，降低token消耗与时延。
   - **方案路径**：扩展现有embedding_cache为语义相似缓存，LLM侧增加summarizer预处理。

6. **RAG评估框架集成**
   - **问题解决**：量化faithfulness、relevancy、覆盖率等指标，支撑持续迭代与验收。
   - **方案路径**：离线评测脚本（RAGAS风格或自定义），接入现有测试与日志看板。

**实施优先级建议**（结合现有架构）：
1. Hybrid Search（召回基础提升，最快见效）
2. 语义Reranker（准确率核心突破）
3. Query Rewrite（多轮对话优化）

**预期效果**：可直接叠加当前动态TopK、MMR、低置信兜底机制。

**验收**：扩展现有[`app_service_retrieval_test.go`](backend/internal/service/app_service_retrieval_test.go)测试，新增RAG指标评测；更新可观测看板。



---

## 第四计划：评估驱动优化规范

> 本章节规范第四阶段六大优化项与 [`backend/eval/`](backend/eval/) 评估框架之间的迭代闭环流程，确保每次优化有数据支撑、有验收门槛、有回滚保护。

---

### 一、评估-优化闭环流程

每次优化迭代严格遵循以下四步闭环：

1. **建立基线（Baseline）**
   - 负责角色：实施工程师
   - 操作：在 [`main`](backend/main.go) 分支对应版本上运行离线评估，生成基线报告。
   - 输出物：[`backend/eval/results/`](backend/eval/results/) 下的基线报告文件，包括同一轮运行生成的 `.json` 与 `.md` 文件。
   - 约束：基线必须在优化分支创建前完成，严禁在已修改代码后补跑基线。

2. **实施优化（Implement）**
   - 负责角色：实施工程师
   - 操作：在功能分支实现优化代码，编写或更新单元测试，并确保 `go test ./...` 通过。
   - 输出物：功能分支代码、更新后的单元测试、本地验证日志。

3. **评估对比（Evaluate）**
   - 负责角色：实施工程师 + Reviewer
   - 操作：在功能分支使用相同数据集运行离线评估，生成对比报告。
   - 输出物：[`backend/eval/results/`](backend/eval/results/) 下的评估报告文件，包括同一轮运行生成的 `.json` 与 `.md` 文件，以及 before/after 指标对比表（须附在 PR 描述中）。

4. **决策上线/回滚（Decision）**
   - 负责角色：Reviewer + 项目负责人
   - 判断标准：所有验收指标达标（见第二节）且无回归（见第四节 Git 工作流）。
   - 上线：Approve PR，合并到 [`main`](backend/main.go)，并归档评估报告。
   - 回滚：关闭 PR，记录失败原因，进入下一轮优化迭代。

---

### 二、各优化项验收标准

所有优化项共享以下**全局指标门槛**（基于 [`backend/eval/offline/metrics.go`](backend/eval/offline/metrics.go) 中定义的指标）：

| 指标 | 目标值 | 说明 |
|------|--------|------|
| HitRate | **≥ 0.70** | 命中率，检索结果包含正确答案片段的用例比例 |
| MRR | **≥ 0.55** | Mean Reciprocal Rank，首命中结果排名倒数均值 |
| Retrieval Latency P95 | **≤ 1200ms** | 检索阶段 P95 时延上限 |
| Generation Latency P95 | **≤ 4500ms** | LLM 生成阶段 P95 时延上限 |

各优化项**额外要求**如下：

#### 1. Hybrid Search（混合检索）

- **核心改动**：[`qdrant_service.go`](backend/internal/service/qdrant_service.go)、[`rag_service.go`](backend/internal/service/rag_service.go) 引入 RRF 融合
- **最小测试集**：≥ 100 条用例，其中关键词精确匹配场景占比 ≥ 30%
- **额外验收**：相比基线 HitRate 相对提升 ≥ 15%；关键词/实体类问题子集 HitRate ≥ 0.75
- **通过判断**：全局门槛全部达标 **且** 关键词子集 HitRate ≥ 0.75 → 通过；任一未达标 → 不通过

#### 2. 语义 Reranker 升级

- **核心改动**：[`app_service.go`](backend/internal/service/app_service.go) 中 [`rerankCandidates()`](backend/internal/service/app_service.go:741) 替换为 cross-encoder 模型
- **最小测试集**：≥ 80 条用例，其中语义相似但主题不同的干扰项场景占比 ≥ 25%
- **额外验收**：MRR 相比基线提升 ≥ 10%；Retrieval Latency P95 增幅 ≤ 200ms
- **通过判断**：全局门槛达标 **且** MRR 提升 ≥ 10% **且** 时延增幅 ≤ 200ms → 通过

#### 3. Query Rewrite + Multi-Query Retrieval

- **核心改动**：[`rag_service.go`](backend/internal/service/rag_service.go) 插入 LLM 查询重写步骤
- **最小测试集**：≥ 80 条用例，其中模糊/多轮对话场景占比 ≥ 40%
- **额外验收**：模糊查询子集 HitRate ≥ 0.65；Generation Latency P95 增幅 ≤ 500ms
- **通过判断**：全局门槛达标 **且** 模糊子集 HitRate ≥ 0.65 → 通过

#### 4. Semantic Chunking 优化

- **核心改动**：[`document_text.go`](backend/internal/util/document_text.go) 切分策略升级为句子/段落边界
- **最小测试集**：≥ 60 条用例，覆盖长文档（>5页）和短文档（<1页）场景各 ≥ 20%
- **额外验收**：HitRate 相比基线不下降（≥ 基线值）；MRR 相比基线不下降
- **通过判断**：全局门槛达标 **且** 指标无回归 → 通过

#### 5. Semantic Cache + Context Compression

- **核心改动**：[`embedding_cache.go`](backend/internal/service/embedding_cache.go) 扩展为语义缓存
- **最小测试集**：≥ 60 条用例，其中重复/相似查询占比 ≥ 30%
- **额外验收**：重复查询缓存命中率 ≥ 80%；Generation Latency P95 相比基线下降 ≥ 10%
- **通过判断**：全局门槛达标 **且** 缓存命中率 ≥ 80% → 通过

#### 6. RAG 评估框架（贯穿）

- **状态**：Phase 1 已完成 ✓
- **最小测试集**：持续扩充（见第三节基线规范）
- **额外验收**：框架本身单元测试 [`metrics_test.go`](backend/eval/offline/metrics_test.go) 全部通过；报告格式符合预期 JSON 结构
- **通过判断**：`go test ./eval/...` 全部通过 → 通过

---

### 三、基线建立规范

#### 3.1 运行基线评估命令

```bash
# 步骤 1：进入 backend 目录
cd backend

# 步骤 2：运行基线评估（mock 模式，用于框架验证）
go run ./eval/cmd/ \
  -dataset eval/data/ground_truth_v1.small.json \
  -output eval/results \
  -mock=true

# 步骤 3：运行基线评估（真实服务模式，需后端服务已启动）
go run ./eval/cmd/ \
  -dataset eval/data/ground_truth_v2.50.json \
  -output eval/results \
  -mock=false
```

#### 3.2 基线报告存档规范

- **存档目录**：`backend/eval/results/`
- **命名规范**：
  - 初始全局基线：`baseline_YYYYMMDD.json` / `baseline_YYYYMMDD.md`
  - 优化项专项基线：`baseline_YYYYMMDD_<优化项标识>.json`（标识：`hybrid-search` / `reranker` / `query-rewrite` / `semantic-chunk` / `semantic-cache` / `eval-framework`）
  - 优化后对比报告：`eval_YYYYMMDD_<分支名简写>.json` / `.md`
- **示例目录结构**：
  ```
  backend/eval/results/
  ├── baseline_20260316.json
  ├── baseline_20260316.md
  ├── baseline_20260320_hybrid-search.json
  ├── baseline_20260320_hybrid-search.md
  ├── eval_20260325_eval-hybrid-search.json
  └── eval_20260325_eval-hybrid-search.md
  ```
- **归档要求**：`baseline_*.json` 和 `baseline_*.md` 必须提交到 Git；`eval_*` 建议提交（PR 附件）

#### 3.3 数据集扩充计划

当前数据集 [`ground_truth_v1.small.json`](backend/eval/data/ground_truth_v1.small.json) 仅含 5 条示例用例，需按以下计划扩充：

| 阶段 | 文件名 | 目标数量 | 场景分布要求 | 完成时机 |
|------|--------|----------|--------------|----------|
| v1（当前） | `ground_truth_v1.small.json` | 5 条 | 框架验证用例 | 已完成 ✓ |
| v2 | `ground_truth_v2.50.json` | 50 条 | easy/medium/hard 各类型，含关键词、语义、多轮场景 | Hybrid Search 优化前 |
| v3 | `ground_truth_v3.200.json` | 200 条 | 新增长文档、模糊查询、中文实体类场景 | Reranker 优化前 |
| v4 | `ground_truth_v4.500.json` | 500 条 | 覆盖全部优化项所需子集，含难例与边界用例 | 第四阶段验收前 |

数据集扩充来源建议：
- 从真实用户查询日志中提取（脱敏后）
- 人工构造各难度代表性问题
- 从知识库文档中自动生成候选问题（LLM 辅助）

---

### 四、优化迭代 Git 工作流

#### 4.1 分支命名规范

所有优化迭代分支统一使用以下格式：`feat/eval-<优化项标识>`

| 优化项 | 分支名 |
|--------|--------|
| Hybrid Search | `feat/eval-hybrid-search` |
| 语义 Reranker 升级 | `feat/eval-reranker` |
| Query Rewrite + Multi-Query | `feat/eval-query-rewrite` |
| Semantic Chunking 优化 | `feat/eval-semantic-chunk` |
| Semantic Cache + Context Compression | `feat/eval-semantic-cache` |
| 评估框架 Phase 2 扩展 | `feat/eval-framework-phase2` |

#### 4.2 PR 要求

每个优化 PR 描述中**必须**包含以下内容：

1. **改动摘要**：核心改动文件列表、算法/策略变化说明（一段话）
2. **评估报告对比表**：before/after 各项指标值、变化量、是否达标（✅/❌）
3. **报告文件路径**：附 `backend/eval/results/eval_<日期>_<分支名>.md` 的 Git 提交链接
4. **测试验证**：提供 `go test ./...` 通过截图或日志输出
5. **数据集信息**：注明评估所用数据集文件名及用例数量

#### 4.3 指标回归保护

以下任一情况触发**强制不通过**，PR 不得合并：

- **HitRate 下降超过 2%**（绝对值降幅 > 0.02）
- **MRR 下降超过 2%**（绝对值降幅 > 0.02）
- **Retrieval Latency P95 增加超过 15%**（相对基线增幅 > 15%）
- **Generation Latency P95 增加超过 15%**（相对基线增幅 > 15%）
- `go test ./...` 有任何失败

示例：基线 HitRate=0.65，优化后 HitRate=0.62，降幅 0.03 > 0.02 → 回归触发，PR 不通过。

---

### 五、Phase 2 评估框架扩展计划

当前 [`backend/eval/`](backend/eval/) 实现了 Phase 1 基础离线评估能力，Phase 2 将在此基础上扩展以下能力：

#### 5.1 Hallucination 检测

- **目标**：检测 LLM 生成答案中是否包含知识库无法支撑的捏造内容
- **实现方案**：
  - 新增 [`backend/eval/offline/hallucination.go`](backend/eval/offline/hallucination.go)（待实现）
  - 判断逻辑：检查答案中的关键声明是否能在检索到的 chunks 中找到来源支撑
  - 指标：`HallucinationRate`（无来源声明占总声明比例）
- **数据集要求**：需在 `GroundTruthCase` 中增加 `expected_facts` 字段标注期望的事实陈述

#### 5.2 Faithfulness 评分

- **目标**：量化 LLM 答案对检索上下文的忠实程度（对标 RAGAS faithfulness 指标）
- **实现方案**：
  - 新增 [`backend/eval/offline/faithfulness.go`](backend/eval/offline/faithfulness.go)（待实现）
  - 评分逻辑：将答案分解为原子陈述，逐条检查是否可从 context chunks 中推导
  - 指标：`FaithfulnessScore`（0.0–1.0，可从 context 推导的陈述比例）
- **依赖**：需要 LLM 辅助分解答案（可复用现有 [`llm_service.go`](backend/internal/service/llm_service.go)）

#### 5.3 在线监控接入

- **目标**：将离线评估指标延伸至线上，实现持续可观测
- **实现方案**：
  - 新增 [`backend/eval/online/`](backend/eval/online/)（待实现）目录，包含：
    - `sampler.go`：线上查询采样（按比例随机采样，写入评估队列）
    - `monitor.go`：定时聚合指标，输出到日志/Prometheus metrics
  - 接入现有 [`logRetrievalMetrics()`](backend/internal/service/app_service.go:963) 的结构化日志
  - 指标看板：HitRate 趋势、P95 时延趋势、低置信度触发率
- **里程碑**：在第四阶段最后一个优化项上线后完成接入

#### 5.4 扩展评估指标（Phase 3 预留）

| 指标 | 说明 | 优先级 |
|------|------|--------|
| `Precision@K` | TopK 中相关结果的精确率 | Phase 3 |
| `Recall@K` | 相关结果被召回的覆盖率 | Phase 3 |
| `AnswerRelevancy` | 答案与问题的相关性评分 | Phase 3 |
| `ContextPrecision` | 检索上下文的精准度 | Phase 3 |
| 并发评估支持 | `MaxConcurrency > 1` 加速批量评估 | Phase 3 |

---

### 六、规范执行检查清单

每次优化迭代前，确认以下检查项全部完成：

- [ ] 已在 `main` 分支运行基线评估并存档报告
- [ ] 功能分支命名符合 `feat/eval-<标识>` 规范
- [ ] 数据集版本满足对应优化项最小要求
- [ ] 优化实现完成，单元测试通过（`go test ./...`）
- [ ] 在功能分支运行评估并生成对比报告
- [ ] before/after 指标对比表已填入 PR 描述
- [ ] 所有全局指标门槛达标（HitRate ≥ 0.70 / MRR ≥ 0.55 / Retrieval P95 ≤ 1200ms / Generation P95 ≤ 4500ms）
- [ ] 无回归触发（HitRate 降幅 ≤ 0.02 / MRR 降幅 ≤ 0.02 / 时延增幅 ≤ 15%）
- [ ] PR 获得至少 1 名 Reviewer Approve
- [ ] 合并后评估报告已归档到 `backend/eval/results/`

---

### 七、最新验证状态（2026-03-19）

1. **真实评估集已生成**
   - 当前使用的数据集为 [`backend/eval/data/ground_truth.generated.json`](backend/eval/data/ground_truth.generated.json)。
   - 当前真实评估集规模为 **12 条用例**，已可用于小样本真实性能验证。

2. **首版真实 Baseline**
   - 报告文件：[`baseline-20260319-132845.json`](backend/eval/results/baseline-20260319-132845.json)
   - 数据集：[`backend/eval/data/ground_truth.generated.json`](backend/eval/data/ground_truth.generated.json)
   - 核心结果：**HitRate = 100%**，**MRR = 0.7917**。

3. **启用 Hybrid Search 后的验证结果**
   - 报告文件：[`baseline-20260319-162028.json`](backend/eval/results/baseline-20260319-162028.json)
   - 数据集：[`backend/eval/data/ground_truth.generated.json`](backend/eval/data/ground_truth.generated.json)
   - 核心结果：**HitRate = 100%**，**MRR = 0.7222**。

4. **当前结论**
   - Hybrid Search 在当前 **12 条小样本真实评估集** 上 **未带来提升**，且 MRR 相比首版真实 Baseline 出现下降。
   - Semantic Reranker 在当前 **12 条小样本真实评估集** 上同样 **未带来提升**，其 MRR 结果与启用 Hybrid Search 后一致，仍低于首版真实 Baseline。
   - 在缺少更大规模、更多样化评估集之前，不应仅凭当前结果将 Hybrid Search 或 Semantic Reranker 视为默认收益项。
   - 下一步应优先继续优化评估集质量，以及查询改写等能力的实际启用条件，再决定是否推进高级能力上线。

### 八、当前默认上线策略（2026-03-19）

#### 1. 默认策略

当前版本默认保持以下高级能力关闭，除非后续真实评估结果明确证明其收益稳定成立：

- `Hybrid Search = off`
- `Semantic Reranker = off`
- `Query Rewrite = off`
  - 在真实评估验证完成前，不作为默认链路启用。
- `Semantic Cache = off`
  - 当前作为后续性能优化灰度项，不纳入默认上线配置。
- `Context Compression = off`
  - 当前作为长上下文场景灰度项，不纳入默认上线配置。

#### 2. 当前默认链路

当前默认上线链路继续采用以下已验证策略：

- dense 检索
- 现有 [`rerankCandidates()`](backend/internal/service/app_service.go:741) 基础逻辑
- MMR 去冗余
- 低置信度扩召回兜底

#### 3. 高级能力启用原则

1. 任何高级能力进入灰度或默认开启前，必须先在真实评估集上完成基线与优化后 before/after 对比验证。
2. 若评估结果中的 MRR 或 HitRate 未优于当前基线，则该能力保持关闭，不进入默认上线配置。
3. 功能已实现并不构成默认开启依据，默认策略必须以真实评估结果作为唯一决策基础。

#### 4. 当前阶段判断

1. 第四阶段相关代码已完成，但当前默认策略不以功能存在性为依据，而以真实评估数据为依据。
2. 基于截至 2026-03-19 的真实评估结果，当前最优上线策略不是全开高级能力，而是保守默认、逐项验证、单项灰度。
3. 后续如需调整默认配置，必须以同数据集或更高质量真实评估集上的明确收益结果作为前置条件。
