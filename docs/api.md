# API Documentation

## Base URL

```
http://localhost:8080/api/v1
```

## Endpoints

### POST /heartbeat

Worker heartbeat endpoint. Workers should send heartbeats every 3 seconds.

**Request:**
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

**Response:**
```json
{
  "status": "ok"
}
```

**Status Codes:**
- 200: Heartbeat accepted
- 400: Invalid request body
- 405: Method not allowed

---

### POST /schedule

Schedule a task to an available worker.

**Request:**
```json
{
  "task_id": "task-001",
  "required_tags": ["gpu", "cuda-12.0"]
}
```

**Response (success):**
```json
{
  "worker_id": "worker-001",
  "address": "192.168.1.100:9000"
}
```

**Response (no available worker):**
```json
{
  "error": "no available worker matching requirements"
}
```

**Status Codes:**
- 200: Task scheduled successfully
- 503: No available worker
- 400: Invalid request body
- 405: Method not allowed

---

### GET /workers

List all workers and their current state.

**Response:**
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

**Status Codes:**
- 200: Success
- 405: Method not allowed

**Worker Status:**
- `online`: Heartbeat received within 10 seconds
- `suspicious`: Heartbeat not received for 10-20 seconds
- `offline`: Heartbeat not received for 20+ seconds

## Scheduling Algorithm

1. **Filter by tags**: Only workers with ALL required tags are considered
2. **Filter by availability**: Offline workers or workers at max capacity are excluded
3. **Sort by load ratio**: `load_ratio = current_tasks / max_tasks`
4. **Select lowest**: Worker with lowest load ratio is selected
5. **Optimistic allocation**: Task count incremented immediately (corrected by next heartbeat)

## Error Handling

All error responses follow this format:

```json
{
  "error": "error message description"
}
```
