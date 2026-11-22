# Go 与 Python 使用 gRPC 的评估和验证

## 结论

可用，已经跑通真实 Go 程序 → 本地 Python gRPC 服务 → DG Transformer 推理 → Go 结果校验。跨语言结果与现有离线入口完全一致（测试绝对误差 0），缺失模型窗口保留 FAILED_PRECONDITION。

此次是独立通信验证：不自动启动服务、不接入网页请求、不写数据库。正式权重和训练数据口径仍未交付，所有推理测试只使用带 `integration_only` 标记的合成样例和随机未训练权重。

## 适用方式

建议用于后台 Go 调度/采集任务调用常驻 Python 推理服务。Python 在启动时加载固定的窗口与权重，后续请求重用模型。Go 负责读取行情和调用，Python 继续复用 dg_final.py 与 predict.py 的特征、标准化和时间对齐逻辑。

| 内容 | 现有离线文件方式 | gRPC 原型 |
| --- | --- | --- |
| 数据交换 | CSV 输入、JSON 文件输出 | Protobuf 类型化请求与响应 |
| 模型 | 每次命令运行加载 | 服务启动加载，常驻重用 |
| 错误 | JSON error code + 退出码 | gRPC status + 业务错误代码 |
| 生命周期 | 适合离线验收与人工复现 | 需管理服务、健康检查、调用期限及资源 |
| 用途 | 保留为模型交付验收入口 | 内部在线推理通信候选 |

gRPC 的收益主要是明确的跨语言协议和服务生命周期；当前单交易对、每分钟一次的任务不需要以吞吐性能作为采用理由。此验证没有做生产性能基准。

## 协议

唯一源文件是 `api/proto/depeg_rpc/inference.proto`；Go/Python 生成文件分别提交到两个仓库。生成后两端应一起更新，兼容性变更通过协议包版本管理。

PredictRequest 包含 schema_version、asset_id、symbol、source、有序分钟 K 线及显式 horizons。K 线时间使用 Unix 秒，OHLC/成交量用十进制文本，以保持原始文本到 Python 特征计算前的精度。请求不能指定服务器文件路径或任意模型。

PredictResponse 保留预测区间、行情截止时刻、模型/标签/特征版本、质量和 `depeg_minute_ratio`。占比用有 presence 的 optional double，Go 能区分缺失与真实零值。Go 复用原有 JSON/domain 校验器，并核对返回窗口、资产及观测时间与请求一致。

## 运行

Python 3.11，在 Depeg-future 中安装依赖并启动（示例目录只包含未训练测试权重）：

```powershell
python -m pip install -r requirements-grpc.txt
python tests/make_smoke_bundle.py
python src/grpc_server.py --model-dir data/inference-smoke/bundle --bind 127.0.0.1:50051
```

另一终端在 Depeg 中调用：

```powershell
go run -buildvcs=false ./cmd/check-grpc -address 127.0.0.1:50051 -input ../Depeg-future/data/inference-smoke/sample_input.csv -horizons 0-3h -timeout 10s
```

实际权重交付后，使用 package_dg.py 生成的目录替换示例目录。多个窗口重复指定 --model-dir；调用时 --horizons 必须都是已加载窗口。更新模型文件后重启 Python 服务，不在请求中热替换权重。

## 超时、错误与部署边界

- 原型只允许回环 IP，默认 127.0.0.1:50051，未提供公网监听。跨机器/容器网络接入前应增加 TLS/mTLS、认证和部署配置。
- 提供标准 gRPC health check，加载模型后才报告 SERVING；Go 调用有显式 timeout，并服从更短的上游 context。
- 单请求最多 4096 根 K 线，4 MiB 消息限制；模型推理只允许一个并行调用，繁忙时返回 RESOURCE_EXHAUSTED，不堆积无界请求。
- 非法输入、缺分钟及数据不足返回 INVALID_ARGUMENT；未加载窗口返回 FAILED_PRECONDITION；推理失败返回 INTERNAL。客户端没有增加自动重试。
- 在特征计算和每个窗口前后检查取消状态。已经进入 PyTorch 的一次原生 forward 不能被 gRPC 强行中断，取消后最多等待该次 forward 完成才释放计算槽；这不是进程级硬终止。

这些语义按官方 [Go 快速入门](https://grpc.io/docs/languages/go/quickstart/)、[Python 快速入门](https://grpc.io/docs/languages/python/quickstart/)、[期限](https://grpc.io/docs/guides/deadlines/)、[取消](https://grpc.io/docs/guides/cancellation/)和[健康检查](https://grpc.io/docs/guides/health-checking/)实现。

## 可复现测试

Go：

```powershell
go test -buildvcs=false ./internal/adapter/pipeline/python ./cmd/check-grpc
go build -buildvcs=false -o ../.runtime/go-bin/check-grpc.exe ./cmd/check-grpc
```

Depeg-future：

```powershell
python -m unittest discover -s tests -p test_grpc.py -v
python tests/check_go_grpc.py --go-client ../.runtime/go-bin/check-grpc.exe --output data/grpc-verification/summary.json
```

Python 网络测试覆盖健康检查、与离线结果一致、模型常驻重用、非法输入、缺失窗口、超时、取消、繁忙和恢复。Go 测试覆盖零值/缺失比值、状态传递、超时、请求响应时间核对及 CSV 时间字段。端到端脚本实际启动 Python 子进程，用 Go 可执行程序连接后清理进程。

## 重新生成绑定

生成工具版本：grpcio-tools 1.76.0、protoc-gen-go 1.36.10、protoc-gen-go-grpc 1.5.1。工具无需作为生产依赖；运行代码已生成并提交。

在 Depeg-future 目录执行（Go 生成工具需位于 PATH）：

```powershell
python -m grpc_tools.protoc -I ../Depeg/api/proto --python_out=src --grpc_python_out=src --go_out=../Depeg --go_opt=module=depeg.local/platform --go-grpc_out=../Depeg --go-grpc_opt=module=depeg.local/platform ../Depeg/api/proto/depeg_rpc/inference.proto
```
