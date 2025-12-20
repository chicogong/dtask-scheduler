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
  "timestamp": 1702540800
}
```

**响应:**
```json
{
  "status": "ok"
}
```

**状态码:**
- 200: 心跳已接收
- 400: 请求体无效
- 405: 方法不允许

---

### POST /schedule

调度一个任务到可用 Worker.

**请求:**
```json
{
  "task_id": "task-001",
  "required_tags": ["gpu", "cuda-12.0"]
}
```

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

**状态码:**
- 200: 调度成功
- 503: 无可用 Worker
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
    "Status": "online"
  }
]
```

**状态码:**
- 200: 成功
- 405: 方法不允许

**Worker 状态:**
- `online`: 10 秒内收到心跳
- `suspicious`: 10-20 秒未收到心跳
- `offline`: 20 秒以上未收到心跳

## 调度算法

1. **标签过滤**: 仅选择包含所有必需标签的 Worker
2. **可用性过滤**: 排除离线或满载的 Worker
3. **负载排序**: `load_ratio = current_tasks / max_tasks`
4. **选择最小**: 选择负载率最低的 Worker
5. **乐观分配**: 任务计数立即加 1 (由下一次心跳校正)

## 错误处理

所有错误响应遵循以下格式:

```json
{
  "error": "error message description"
}
```
