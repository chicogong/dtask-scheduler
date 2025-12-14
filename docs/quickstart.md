# Quick Start Guide

## Prerequisites

- Go 1.21 or later
- Network connectivity between scheduler and workers

## Installation

### Clone and Build

```bash
git clone https://github.com/chicogong/dtask-scheduler.git
cd dtask-scheduler
go build -o bin/scheduler ./cmd/scheduler
go build -o bin/worker ./cmd/worker
```

## Running Locally

### Terminal 1: Start Scheduler

```bash
./bin/scheduler --port=8080
```

Expected output:
```
dtask-scheduler starting...
Scheduler listening on port 8080
```

### Terminal 2: Start GPU Worker

```bash
./bin/worker \
  --id=worker-gpu-001 \
  --addr=localhost:9001 \
  --tags=gpu,cuda-12.0 \
  --max-tasks=30 \
  --scheduler=http://localhost:8080
```

### Terminal 3: Start CPU Worker

```bash
./bin/worker \
  --id=worker-cpu-001 \
  --addr=localhost:9002 \
  --tags=cpu,avx2 \
  --max-tasks=30 \
  --scheduler=http://localhost:8080
```

### Terminal 4: Test Scheduling

```bash
# List workers
curl http://localhost:8080/api/v1/workers | jq

# Schedule GPU task
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-001","required_tags":["gpu"]}' | jq

# Schedule CPU task
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-002","required_tags":["cpu"]}' | jq

# Schedule any task (load balancing)
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-003","required_tags":[]}' | jq
```

## Production Deployment

### Scheduler (on dedicated machine)

```bash
# Production config
export PORT=8080
export LOG_LEVEL=info

./scheduler --port=$PORT
```

### Workers (on compute machines)

```bash
# GPU worker example
./worker \
  --id=$(hostname)-gpu \
  --addr=$(hostname -I | awk '{print $1}'):9000 \
  --tags=gpu,cuda-12.0,$(nvidia-smi --query-gpu=gpu_name --format=csv,noheader | tr ' ' '-') \
  --max-tasks=30 \
  --scheduler=http://scheduler.example.com:8080

# CPU worker example
./worker \
  --id=$(hostname)-cpu \
  --addr=$(hostname -I | awk '{print $1}'):9000 \
  --tags=cpu,$(lscpu | grep 'Model name' | awk -F: '{print $2}' | tr ' ' '-') \
  --max-tasks=50 \
  --scheduler=http://scheduler.example.com:8080
```

### Using systemd

Create `/etc/systemd/system/dtask-worker.service`:

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

Enable and start:

```bash
sudo systemctl enable dtask-worker
sudo systemctl start dtask-worker
sudo systemctl status dtask-worker
```

## Monitoring

### Check Worker Status

```bash
watch -n 1 'curl -s http://localhost:8080/api/v1/workers | jq'
```

### Logs

Scheduler and worker logs go to stdout. Redirect to file:

```bash
./scheduler 2>&1 | tee scheduler.log
./worker 2>&1 | tee worker.log
```

## Troubleshooting

### Worker not showing up

1. Check network connectivity: `curl http://scheduler:8080/api/v1/workers`
2. Check worker logs for heartbeat errors
3. Verify scheduler URL is correct

### Scheduling fails with "no available worker"

1. Check if workers are online: `curl http://scheduler:8080/api/v1/workers`
2. Verify required tags match worker tags
3. Check if all workers are at max capacity

### Worker shows as "offline"

1. Worker hasn't sent heartbeat in 20+ seconds
2. Check worker process is running: `ps aux | grep worker`
3. Check network connectivity
4. Restart worker
