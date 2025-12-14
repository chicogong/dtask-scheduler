# dtask-scheduler

A distributed CPU/GPU task scheduler for large-scale batch jobs across thousands of machines.

## Features

- **Zero dependencies**: No Redis, Kafka, or other middleware required
- **High performance**: Sub-millisecond scheduling latency
- **Load balancing**: Automatic task distribution based on worker load
- **Resource matching**: Tag-based worker filtering (GPU, CPU, CUDA versions, etc.)
- **Simple deployment**: Single binary for scheduler and worker

## Architecture

```
Client → Scheduler → Worker Pool (500+ machines)
         ↑
         └─ Heartbeat (every 3s)
```

See [Design Document](docs/plans/2025-12-14-distributed-scheduler-design.md) for details.

## Quick Start

### 1. Build

```bash
go build -o scheduler ./cmd/scheduler
go build -o worker ./cmd/worker
```

### 2. Start Scheduler

```bash
./scheduler --port=8080
```

### 3. Start Workers

```bash
# GPU worker
./worker --id=worker-001 --addr=192.168.1.100:9000 --tags=gpu,cuda-12.0 --max-tasks=30

# CPU worker
./worker --id=worker-002 --addr=192.168.1.101:9000 --tags=cpu,avx2 --max-tasks=30
```

### 4. Schedule Tasks

```bash
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-001","required_tags":["gpu"]}'
```

Response:
```json
{
  "worker_id": "worker-001",
  "address": "192.168.1.100:9000"
}
```

## API Documentation

See [API Documentation](docs/api.md)

## Testing

```bash
# Unit tests
go test ./...

# Integration tests
go test ./tests -v
```

## Roadmap

- [x] MVP: Single scheduler + heartbeat + basic scheduling
- [ ] High availability: Standby scheduler with failover
- [ ] Monitoring: Metrics and alerting
- [ ] Tag indexing: Faster resource filtering
- [ ] Queue: Wait queue for resource shortage

## License

MIT License - see [LICENSE](LICENSE) for details
