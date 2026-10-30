# public-v1 公开评测夹具

该目录包含公开、短小、可重复上传的科技与 AI 事实夹具，供离线回归和浏览器级 E2E 使用。

## 文件

- `ai-tech-facts.md`：Qdrant、Hugging Face Transformers、scikit-learn、PyTorch 和 TensorFlow 的最小事实集合。
- `manifest.json`：稳定的文件、段落锚点、评测用例和公开来源映射。
- `legacy-rag.md`、`evaluation-project.md`、`records.csv`：可公开复用的合成业务与表格样例。

## 本地验证与报告比较

在 `backend/` 执行 `go test ./eval/...`，校验 51 条案例、资料校验和及指标逻辑。
评测入口支持 `-fixture-manifest eval/fixtures/public-v1/manifest.json`，在执行前验证资料；mock 只验证评测链路，不代表真实模型质量。

已有两份报告时执行：

```bash
go run ./eval/cmd/compare_reports -baseline baseline.json -candidate candidate.json -fail-on-regression
```

指标下降、证据退化或候选缺少基线案例均返回非零。词法忠实度仅作辅助指标，不能当作语义真值。

## 数据边界

- 仅包含公开技术文档中的短事实和来源 URL。
- 不包含网页 HTML、抓取缓存、用户上传文件、知识库快照、账号、Token 或运行结果。
- 评测用例使用 `answer_snippets` 做事实命中校验，不固化运行时随机知识库 ID、文档 ID 或 Chunk ID。
- 来源页面内容变化时，应重新执行校验；无法直接支撑答案的样本不得自动标记为正式通过。
