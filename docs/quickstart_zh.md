# 快速开始指南

## 前置条件

- Go 1.21 或更高版本
- 调度器与 Worker 之间网络互通

## 安装

### 克隆并构建

```bash
git clone https://github.com/chicogong/dtask-scheduler.git
cd dtask-scheduler
go build -o bin/scheduler ./cmd/scheduler
go build -o bin/worker ./cmd/worker
```

## 本地运行

### 终端 1: 启动调度器

```bash
./bin/scheduler --port=8080
```

预期输出:
```
dtask-scheduler starting...
Scheduler listening on port 8080
```

### 终端 2: 启动 GPU Worker

```bash
./bin/worker \
  --id=worker-gpu-001 \
  --addr=localhost:9001 \
  --tags=gpu,cuda-12.0 \
  --max-tasks=30 \
  --scheduler=http://localhost:8080
```

### 终端 3: 启动 CPU Worker

```bash
./bin/worker \
  --id=worker-cpu-001 \
  --addr=localhost:9002 \
  --tags=cpu,avx2 \
  --max-tasks=30 \
  --scheduler=http://localhost:8080
```

### 终端 4: 测试调度

```bash
# 查看 Worker 列表
curl http://localhost:8080/api/v1/workers | jq

# 调度 GPU 任务
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-001","required_tags":["gpu"]}' | jq

# 调度 CPU 任务
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-002","required_tags":["cpu"]}' | jq

# 调度任意任务 (负载均衡)
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-003","required_tags":[]}' | jq
```

## 生产部署

### 调度器 (独立机器)

```bash
# 生产配置示例
export PORT=8080

./scheduler --port=$PORT
```

### Worker (计算节点)

```bash
# GPU Worker 示例
./worker \
  --id=$(hostname)-gpu \
  --addr=$(hostname -I | awk '{print $1}'):9000 \
  --tags=gpu,cuda-12.0,$(nvidia-smi --query-gpu=gpu_name --format=csv,noheader | tr ' ' '-') \
  --max-tasks=30 \
  --scheduler=http://scheduler.example.com:8080

# CPU Worker 示例
./worker \
  --id=$(hostname)-cpu \
  --addr=$(hostname -I | awk '{print $1}'):9000 \
  --tags=cpu,$(lscpu | grep 'Model name' | awk -F: '{print $2}' | tr ' ' '-') \
  --max-tasks=50 \
  --scheduler=http://scheduler.example.com:8080
```

### 使用 systemd

创建 `/etc/systemd/system/dtask-worker.service`:

```ini
[Unit]
Description=dtask-scheduler worker
After=network.target

[Service]
Type=simple
User=dtask
WorkingDirectory=/opt/dtask
ExecStart=/opt/dtask/worker \
  --id=%H \
  --addr=%H:9000 \
  --tags=gpu,cuda-12.0 \
  --max-tasks=30 \
  --scheduler=http://scheduler:8080
Restart=always

[Install]
WantedBy=multi-user.target
```

启用并启动:

```bash
sudo systemctl enable dtask-worker
sudo systemctl start dtask-worker
sudo systemctl status dtask-worker
```

## 高可用（主备模式）

调度器支持主备 HA 对。主节点定期将 Worker 状态同步到备节点；备节点对主节点进行健康检查，主节点不可达时自动晋升为主节点。

### 终端 1：启动主调度器

```bash
./bin/scheduler \
  --port=8080 \
  --role=master \
  --peer=http://localhost:8081
```

### 终端 2：启动备调度器

```bash
./bin/scheduler \
  --port=8081 \
  --role=standby \
  --peer=http://localhost:8080
```

### 启动 Worker 并开启双发心跳

通过 `--standby` 参数，Worker 将同时向主备两个调度器发送心跳：

```bash
./bin/worker \
  --id=worker-gpu-001 \
  --addr=localhost:9001 \
  --tags=gpu,cuda-12.0 \
  --max-tasks=30 \
  --scheduler=http://localhost:8080 \
  --standby=http://localhost:8081
```

**故障切换行为：** 备节点每 2 秒（可通过 `--failover-interval` 配置）请求一次 `GET http://localhost:8080/healthz`。连续 3 次失败（可通过 `--failover-threshold` 配置）后，备节点自动晋升为主节点并开始接受调度请求。

备节点角色期间，`POST /api/v1/schedule` 将返回 `503`。

## 可观测性

### 存活探针

```bash
curl http://localhost:8080/healthz
# {"status":"ok"}
```

### Prometheus 指标

```bash
curl http://localhost:8080/metrics
```

返回 Prometheus 文本格式，包含计数器（`dtask_schedule_requests_total`、`dtask_schedule_successes_total`、`dtask_schedule_failures_total`、`dtask_heartbeats_total`）、调度延迟仪表盘（`dtask_schedule_latency_avg_ms`、`dtask_schedule_latency_max_ms`）以及 Worker/容量仪表盘。

### JSON 统计汇总

```bash
curl http://localhost:8080/stats | jq
```

返回各状态 Worker 数量、容量总量、集群平均负载率以及调度统计计数。

### 查看 Worker 状态

```bash
watch -n 1 'curl -s http://localhost:8080/api/v1/workers | jq'
```

### 日志

调度器与 Worker 日志输出到 stdout，可重定向到文件：

```bash
./scheduler 2>&1 | tee scheduler.log
./worker 2>&1 | tee worker.log
```

## 排障

### Worker 未显示

1. 检查网络连通性: `curl http://scheduler:8080/api/v1/workers`
2. 检查 Worker 日志是否有心跳错误
3. 确认 Scheduler URL 是否正确

### 调度失败并提示 "no available worker"

1. 检查 Worker 是否在线: `curl http://scheduler:8080/api/v1/workers`
2. 确认任务所需标签与 Worker 标签匹配
3. 检查 Worker 是否达到最大并发

### Worker 显示为 "offline"

1. 20 秒以上未收到心跳
2. 检查 Worker 进程是否运行: `ps aux | grep worker`
3. 检查网络连通性
4. 重启 Worker
