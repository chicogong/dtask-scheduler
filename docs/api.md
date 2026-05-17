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
  "timestamp": 1702540800,
  "metrics": {
    "cpu_usage": 0.45,
    "memory_usage": 0.60,
    "gpu_usage": 0.80
  }
}
```

The `metrics` object is optional. Each field is a float in the range `0.0`–`1.0`.

**Validation rules (400 returned on violation):**
- `worker_id` is required and must be non-empty.
- `max_tasks` must be >= 0.
- `current_tasks` must be >= 0.

**Response:**
```json
{
  "status": "ok"
}
```

**Status Codes:**
- 200: Heartbeat accepted
- 400: Invalid request body or validation failure
- 405: Method not allowed

---

### POST /schedule

Schedule a task to an available worker.

**Request:**
```json
{
  "task_id": "task-001",
  "required_tags": ["gpu", "cuda-12.0"],
  "max_wait_ms": 5000
}
```

`max_wait_ms` is optional (integer). When `> 0` and no worker is currently available, the scheduler blocks up to that many milliseconds waiting for a worker to free capacity before returning 503. The value is hard-capped at `60000` ms. When omitted or `0`, the scheduler returns 503 immediately (fail-fast, existing behavior).

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

**Response (scheduler is in standby role):**
```json
{
  "error": "scheduler role is standby, endpoint requires master"
}
```

**Status Codes:**
- 200: Task scheduled successfully
- 503: No available worker, or scheduler is in standby role
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
    "Status": "online",
    "Metrics": {"cpu_usage": 0.45, "memory_usage": 0.60, "gpu_usage": 0.80}
  }
]
```

**Status Codes:**
- 200: Success
- 405: Method not allowed

**Worker Status:**
- `online`: Heartbeat received within the suspicious threshold (default 10s)
- `suspicious`: Heartbeat not received for between the suspicious and offline thresholds (default 10–20s)
- `offline`: Heartbeat not received beyond the offline threshold (default 20s+)

---

### GET /healthz

Liveness probe. Returns immediately with no authentication required.

**Response:**
```json
{"status": "ok"}
```

**Status Codes:**
- 200: Scheduler is alive

---

### GET /metrics

Prometheus text exposition format metrics.

**Response** (`Content-Type: text/plain; version=0.0.4`):

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

**Status Codes:**
- 200: Success

---

### GET /stats

JSON aggregate of worker counts, capacity, and scheduling counters.

**Response:**
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

**Status Codes:**
- 200: Success

---

### POST /api/v1/sync

Internal replication endpoint used by the master to push its worker-state table to the standby. Not intended for external use.

- A **standby** scheduler accepts `POST /api/v1/sync` and replaces its in-memory worker table with the posted snapshot.
- A **master** scheduler rejects it (the endpoint requires the standby role).

**Status Codes:**
- 200: Sync accepted (standby)
- 503: Rejected — scheduler is in master role
- 400: Invalid request body
- 405: Method not allowed (non-POST)

## Scheduling Algorithm

1. **Filter by tags**: A tag inverted index intersects the candidate sets of the required tags, so filtering scales with the number of matching workers instead of the whole pool
2. **Filter by availability**: Offline workers or workers at max capacity are excluded
3. **Sort by load ratio**: `load_ratio = current_tasks / max_tasks`
4. **Spread**: All workers whose load ratio is within 0.05 of the minimum form a candidate set; one is chosen at random to avoid hot-spotting
5. **Optimistic allocation**: Task count incremented immediately (corrected by next heartbeat)
6. **Wait queue (optional)**: If `max_wait_ms > 0` and no worker is available, the scheduler blocks up to that many milliseconds (hard cap: 60000ms) for a worker to free capacity before returning 503

## Error Handling

All error responses follow this format:

```json
{
  "error": "error message description"
}
```
