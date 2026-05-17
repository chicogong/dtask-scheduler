# API 文档

## 基地址

```
http://localhost:8080/api/v1
```

## 端点

### POST /heartbeat

Worker 心跳接口. Worker 应每 3 秒发送一次心跳.

**请求:**
```json
{
  "worker_id": "worker-001",
  "address": "192.168.1.100:9000",
  "resource_tags": ["gpu", "cuda-12.0", "cpu-64core"],
  "max_tasks": 30,
  "current_tasks": 15,
  "timestamp": 1702540800,
  "metrics": {
    "cpu_usage": 0.45,
    "memory_usage": 0.60,
    "gpu_usage": 0.80
  }
}
```

`metrics` 为可选字段，每个子字段为 `0.0`–`1.0` 范围内的浮点数。

**校验规则（违反则返回 400）：**
- `worker_id` 必填且不能为空。
- `max_tasks` 必须 >= 0。
- `current_tasks` 必须 >= 0。

**响应:**
```json
{
  "status": "ok"
}
```

**状态码:**
- 200: 心跳已接收
- 400: 请求体无效或校验失败
- 405: 方法不允许

---

### POST /schedule

调度一个任务到可用 Worker.

**请求:**
```json
{
  "task_id": "task-001",
  "required_tags": ["gpu", "cuda-12.0"],
  "max_wait_ms": 5000
}
```

`max_wait_ms` 为可选整数字段。当 `> 0` 且当前无可用 Worker 时，调度器最多阻塞该毫秒数等待 Worker 释放容量，再返回 503。该值上限为 `60000` ms。省略或为 `0` 时立即失败（沿用原有行为）。

**响应 (成功):**
```json
{
  "worker_id": "worker-001",
  "address": "192.168.1.100:9000"
}
```

**响应 (无可用 Worker):**
```json
{
  "error": "no available worker matching requirements"
}
```

**响应 (调度器为备节点角色):**
```json
{
  "error": "scheduler role is standby, endpoint requires master"
}
```

**状态码:**
- 200: 调度成功
- 503: 无可用 Worker，或调度器为备节点角色
- 400: 请求体无效
- 405: 方法不允许

---

### GET /workers

列出所有 Worker 及其状态.

**响应:**
```json
[
  {
    "WorkerID": "worker-001",
    "Address": "192.168.1.100:9000",
    "ResourceTags": ["gpu", "cuda-12.0"],
    "MaxTasks": 30,
    "CurrentTasks": 15,
    "Available": 15,
    "LastHeartbeat": "2025-12-14T16:30:00Z",
    "Status": "online",
    "Metrics": {"cpu_usage": 0.45, "memory_usage": 0.60, "gpu_usage": 0.80}
  }
]
```

**状态码:**
- 200: 成功
- 405: 方法不允许

**Worker 状态:**
- `online`: 在 suspicious 阈值内收到心跳（默认 10 秒）
- `suspicious`: 超过 suspicious 阈值但未超过 offline 阈值（默认 10–20 秒）
- `offline`: 超过 offline 阈值（默认 20 秒以上）

---

### GET /healthz

存活探针。无需鉴权，立即返回。

**响应:**
```json
{"status": "ok"}
```

**状态码:**
- 200: 调度器存活

---

### GET /metrics

Prometheus 文本格式指标。

**响应** (`Content-Type: text/plain; version=0.0.4`):

```
# HELP dtask_schedule_requests_total Total number of schedule requests received
# TYPE dtask_schedule_requests_total counter
dtask_schedule_requests_total 1500

# HELP dtask_schedule_successes_total Total number of successfully scheduled tasks
# TYPE dtask_schedule_successes_total counter
dtask_schedule_successes_total 1480

# HELP dtask_schedule_failures_total Total number of failed schedule attempts
# TYPE dtask_schedule_failures_total counter
dtask_schedule_failures_total 20

# HELP dtask_heartbeats_total Total number of heartbeats received
# TYPE dtask_heartbeats_total counter
dtask_heartbeats_total 45000

# HELP dtask_schedule_latency_avg_ms Average scheduling latency in milliseconds
# TYPE dtask_schedule_latency_avg_ms gauge
dtask_schedule_latency_avg_ms 0.42

# HELP dtask_schedule_latency_max_ms Maximum scheduling latency in milliseconds
# TYPE dtask_schedule_latency_max_ms gauge
dtask_schedule_latency_max_ms 3.10

# HELP dtask_workers Number of workers by status
# TYPE dtask_workers gauge
dtask_workers{status="online"} 48
dtask_workers{status="suspicious"} 2
dtask_workers{status="offline"} 0

# HELP dtask_capacity_total Total task capacity across all workers
# TYPE dtask_capacity_total gauge
dtask_capacity_total 1500

# HELP dtask_capacity_used Currently used task slots
# TYPE dtask_capacity_used gauge
dtask_capacity_used 720

# HELP dtask_capacity_available Currently available task slots
# TYPE dtask_capacity_available gauge
dtask_capacity_available 780
```

**状态码:**
- 200: 成功

---

### GET /stats

Worker 数量、容量及调度统计的 JSON 汇总。

**响应:**
```json
{
  "workers": {
    "online": 48,
    "suspicious": 2,
    "offline": 0,
    "total": 50
  },
  "capacity": {
    "total_slots": 1500,
    "used_slots": 720,
    "available_slots": 780
  },
  "avg_load_ratio": 0.48,
  "metrics": {
    "schedule_requests": 1500,
    "schedule_successes": 1480,
    "schedule_failures": 20,
    "heartbeats": 45000,
    "schedule_latency_avg_ms": 0.42,
    "schedule_latency_max_ms": 3.10
  }
}
```

**状态码:**
- 200: 成功

---

### POST /api/v1/sync

主节点向备节点推送 Worker 状态表的内部同步端点，不供外部调用使用。

- **备节点**接受 `POST /api/v1/sync`，用收到的快照替换内存中的 Worker 状态表。
- **主节点**会拒绝该请求（该端点要求 standby 角色）。

**状态码:**
- 200: 同步成功（备节点）
- 503: 被拒绝 — 调度器当前为 master 角色
- 400: 请求体无效
- 405: 方法不允许（非 POST）
- 400: 请求体无效

## 调度算法

1. **标签过滤**: 利用标签倒排索引对所有必需标签取交集，过滤开销只与匹配的 Worker 数量相关，而非整个集群规模
2. **可用性过滤**: 排除离线或满载的 Worker
3. **负载排序**: `load_ratio = current_tasks / max_tasks`
4. **热点分散**: 负载率在最小值 0.05 以内的 Worker 组成候选集，随机选取其中一个，避免任务堆积到单一节点
5. **乐观分配**: 任务计数立即加 1（由下一次心跳校正）
6. **等待队列（可选）**: 若 `max_wait_ms > 0` 且无可用 Worker，调度器最多阻塞该毫秒数（上限 60000ms）等待 Worker 释放容量，否则返回 503

## 错误处理

所有错误响应遵循以下格式:

```json
{
  "error": "error message description"
}
```
