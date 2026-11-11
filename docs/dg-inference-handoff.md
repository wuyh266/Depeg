# DG Transformer 离线交付联调

Python 工程的输入输出约定见相邻 `Depeg-future/INFERENCE.md`。它提供 `dg-inference/v1` JSON；此处新增 `python.DecodeResult` 和只读验收命令：

```powershell
go run ./cmd/check-prediction -input ../Depeg-future/data/inference-smoke/prediction.json
```

成功时解析成 domain/risk.Prediction，保留窗口、模型版本、来源、观测时间、生成时间、占比和质量。显式零占比是合法结果，null 或缺失占比被拒绝。错误 JSON 转为带 Code/Message 的 InferenceFailure。

当前仅允许 OKX USDC/USDT，检查标签/特征版本、窗口与绝对起止时间一致性，拒绝不支持版本、重复窗口、缺字段和无效数值。`integration_only` 会原样保留，不能作为已验证模型风险展示。

测试命令：

```powershell
go test ./internal/adapter/pipeline/python ./cmd/check-prediction
```

这一步完成的是离线文件交付验收。当前没有启用 Python HTTP 服务、风险结果持久化或网站预测展示，`/api/v1/risks` 仍保持现有 503 行为。真实 checkpoint、对应 scaler、训练数据口径和实际窗口尚未交付，不能声称生产模型已接入。
