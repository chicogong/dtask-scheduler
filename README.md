# dtask-scheduler

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Tests](https://img.shields.io/badge/Tests-31%20Passed-brightgreen)](tests/)
[![Coverage](https://img.shields.io/badge/Coverage-84--100%25-brightgreen)](tests/)

[English](README.md) | [简体中文](README_ZH.md)

A distributed CPU/GPU task scheduler for large-scale batch jobs across thousands of machines.

## Features

- **Zero dependencies**: No Redis, Kafka, or other middleware required
- **High performance**: Sub-millisecond scheduling latency (< 1ms)
- **Load balancing**: Automatic task distribution based on worker load
- **Resource matching**: Tag-based worker filtering (GPU, CPU, CUDA versions, etc.)
- **Simple deployment**: Single binary for scheduler and worker

## Performance Metrics

| Metric | Value | Description |
|--------|-------|-------------|
| **Scheduling Latency** | < 1ms | Time to assign task to worker |
| **Throughput** | 1000+ req/s | Scheduling requests per second |
| **Worker Scale** | 500+ machines | Tested worker pool size |
| **Heartbeat Overhead** | 33KB/s | Network bandwidth for 500 workers |
| **Memory Usage** | < 3MB | Scheduler memory footprint for 500 workers |
| **Timeout Detection** | 10s/20s | Suspicious/Offline thresholds |
| **Test Coverage** | 84-100% | Unit and integration test coverage |

## Status

| Component | Status | Description |
|-----------|--------|-------------|
| Core Scheduler | ✅ Production Ready | Single scheduler with in-memory state |
| Worker Agent | ✅ Production Ready | Heartbeat sender with graceful shutdown |
| Resource Filtering | ✅ Production Ready | Tag-based worker matching |
| Load Balancing | ✅ Production Ready | Load ratio-based selection |
| HTTP API | ✅ Production Ready | 3 endpoints with error handling |
| Integration Tests | ✅ Passing | 31 tests, 100% pass rate |
| High Availability | 🚧 Planned | Standby scheduler with failover |
| Monitoring | 🚧 Planned | Metrics and alerting |
| Tag Indexing | 🚧 Planned | Performance optimization |

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
