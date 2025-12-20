# 分布式任务调度器设计文档

**日期**: 2025-12-14
**版本**: 1.0
**状态**: 已验证

## 概述

dtask-scheduler 是一个面向大规模批处理任务的分布式CPU/GPU任务调度系统，支持跨数千台异构机器的统一调度与负载均衡。

### 核心目标

- **统一调度**: 中心化调度决策，全局视图优化
- **零依赖**: 不依赖第三方中间件（Redis/Kafka等）
- **高性能**: 微秒级调度延迟，支持每秒数千次请求
- **负载均衡**: 保证任务在Worker间均匀分配

### 系统规模

- Worker机器数: 500台（可扩展到数千台）
- 单机并发任务数: 30个
- 资源类型: 异构（GPU/CPU混合）

---

## 整体架构

系统采用**主备调度器 + Worker Agent**的中心化架构。

### 组件划分

```
┌─────────────┐      调度请求      ┌──────────────┐
│   Client    │ ──────────────────> │ Master       │
│             │ <────────────────── │ Scheduler    │
└─────────────┘    返回Worker地址   └──────────────┘
                                           │
                                           │ 状态同步
                                           ↓
                                    ┌──────────────┐
                                    │ Standby      │
                                    │ Scheduler    │
                                    └──────────────┘
                                           ↑
                                           │ 心跳副本
      ┌────────────────────────────────────┴─────────┐
      │                    心跳                       │
      ↓                                               ↓
┌──────────┐  ┌──────────┐  ┌──────────┐      ┌──────────┐
│ Worker 1 │  │ Worker 2 │  │ Worker 3 │ ...  │ Worker N │
└──────────┘  └──────────┘  └──────────┘      └──────────┘
```

### 1. 调度器（Scheduler）

**主调度器（Master）**
- 接收客户端调度请求
- 维护Worker内存状态表
- 执行调度决策（资源匹配 + 负载排序）
- 异步同步状态到备调度器

**备调度器（Standby）**
- 接收Worker心跳副本
- 保持与主调度器状态同步（延迟 < 1秒）
- 健康检查主调度器（每2秒）
- 主挂掉时秒级接管（< 10秒）

### 2. Worker Agent

部署在每台机器上的代理进程，职责：
- 定期上报心跳（负载、资源标签、容量）
- 接收并执行调度器分配的任务
- 任务完成后释放容量
- 心跳双发：同时发送给主备调度器

### 3. 客户端（Client）

- 向调度器发送任务调度请求
- 请求包含：任务ID、资源需求标签（如 `["gpu", "cuda-12.0"]`）
- 收到Worker地址后，直接与Worker通信执行任务

### 数据流

1. **Worker → 调度器**: 心跳（每3秒）
2. **Client → 主调度器**: 调度请求
3. **主调度器 → Client**: 返回最优Worker地址
4. **Client → Worker**: 直接发送任务执行指令

---

## Worker心跳机制

### 心跳Payload

```json
{
  "worker_id": "worker-001",
  "timestamp": 1702540800,
  "resource_tags": ["gpu", "cuda-12.0", "cpu-64core"],
  "capacity": {
    "max_tasks": 30,
    "current_tasks": 15,
    "available": 15
  },
  "metrics": {
    "cpu_usage": 0.65,
    "memory_usage": 0.48,
    "gpu_usage": 0.70
  }
}
```

### 心跳策略

- **频率**: 每3秒发送一次
- **目标**: 同时发送给主备调度器（UDP或HTTP）
- **失败处理**: 单次失败不重试，等下个周期
- **网络开销**: 500台 × 200字节 × 0.33次/秒 ≈ 33KB/s

### 超时检测（调度器侧）

- **10秒未收到**: 标记为"可疑"（降低调度优先级）
- **20秒未收到**: 标记为"下线"，移出调度池
- **恢复心跳**: 自动加回调度池

### 批量处理优化

调度器每1秒批量处理一次心跳队列（而非每个心跳立即处理），降低锁竞争，提高吞吐。

---

## 调度算法（保证均匀分配）

### 调度流程

当客户端请求到达时：

#### 1. 资源标签过滤

```go
required_tags := ["gpu", "cuda-12.0"]

// 从内存状态表过滤
candidates := []
for _, worker := range workers {
    if worker.Status != "online" || worker.Available == 0 {
        continue // 跳过下线或满载的Worker
    }
    if containsAllTags(worker.ResourceTags, required_tags) {
        candidates = append(candidates, worker)
    }
}
```

#### 2. 负载排序（均匀性保证）

```go
// 计算负载率
for _, worker := range candidates {
    worker.LoadRatio = float64(worker.CurrentTasks) / float64(worker.MaxTasks)
}

// 按负载率升序排序（最空闲的排最前）
sort.Slice(candidates, func(i, j int) bool {
    return candidates[i].LoadRatio < candidates[j].LoadRatio
})
```

**示例**:
- Worker A: 15/30 = 0.50
- Worker B: 10/30 = 0.33
- Worker C: 20/30 = 0.67
- 排序结果: B → A → C，选择 Worker B

#### 3. 最终选择

- **优先策略**: 选择负载率最低的Worker
- **打散策略**（可选）: 如果前3名负载率差距 < 5%，随机选一个（避免热点）

#### 4. 乐观分配

```go
selectedWorker := candidates[0]

// 立即返回Worker地址给客户端
response := &ScheduleResponse{
    WorkerID:  selectedWorker.WorkerID,
    Address:   selectedWorker.Address,
}

// 同时乐观更新内存状态（不等待确认）
selectedWorker.CurrentTasks += 1
selectedWorker.Available -= 1

// 真实负载会在下次心跳（最多3秒后）校正
```

### 性能指标

- **调度延迟**: < 1ms（内存过滤 + 排序500条记录）
- **吞吐量**: 支持每秒数千次调度请求
- **均匀性**: 负载率排序确保始终选择最空闲机器

---

## 主备切换机制

### 状态同步（主 → 备）

```
主调度器                        备调度器
    │                              │
    │ ─── 心跳更新批次 ────────────> │
    │     (异步,非阻塞)              │
    │                              │
    │ <──── 同步ACK ──────────────  │
```

- **同步方式**: 主调度器每收到一批心跳后，异步发送增量状态
- **同步内容**: Worker状态变更（上线/下线/负载更新）
- **同步延迟**: < 1秒（不阻塞调度主流程）

### 故障检测

**备调度器主动探测**:
- 每2秒向主调度器发送健康检查（TCP ping）
- 连续3次失败（6秒）→ 判定主挂掉

**Worker视角**:
- Worker同时向主备发心跳
- 如果主连续失败，也可感知异常

### Failover流程

```
备调度器检测到主挂掉
    ↓
切换为主模式（开始接收调度请求）
    ↓
广播通知客户端新地址（DNS/VIP切换）
    ↓
完成切换（总耗时 < 10秒）
```

### 脑裂预防

**简单方案**:
- 调度器启动时通过配置文件指定角色（master/standby）
- 如果旧主恢复，检测到备已升主，自动降级为备

**租约机制**（可选）:
- 使用外部文件锁确保同一时刻只有一个主
- 主定期续约，超时后锁自动释放

### 恢复后同步

- 新主上线后，旧主（或新备）从新主全量同步Worker状态表
- 全量同步完成后，切换为增量同步模式

---

## 数据结构设计

### 1. Worker状态表（主索引）

```go
type WorkerState struct {
    WorkerID      string        // "worker-001"
    Address       string        // "192.168.1.100:9000"
    ResourceTags  []string      // ["gpu", "cuda-12.0", "cpu-64core"]
    MaxTasks      int           // 30
    CurrentTasks  int           // 15
    Available     int           // 15
    LastHeartbeat time.Time     // 最后心跳时间
    Status        string        // "online" | "suspicious" | "offline"
    Metrics       WorkerMetrics // CPU/GPU/内存使用率
}

type WorkerMetrics struct {
    CPUUsage    float64 // 0.65
    MemoryUsage float64 // 0.48
    GPUUsage    float64 // 0.70
}

// 主存储：Map, O(1)查询
var (
    workers     = make(map[string]*WorkerState) // key: WorkerID
    workersLock sync.RWMutex                   // 读写锁保护
)
```

### 2. 标签倒排索引（加速资源过滤）

```go
// 标签 → Worker ID集合
var tagIndex = make(map[string]map[string]bool)

// 示例数据
// tagIndex["gpu"] = {"worker-001": true, "worker-003": true}
// tagIndex["cuda-12.0"] = {"worker-001": true}

// 查询时：取多个标签集合的交集
func filterByTags(requiredTags []string) []string {
    if len(requiredTags) == 0 {
        return getAllWorkerIDs()
    }

    // 从第一个标签开始
    candidates := tagIndex[requiredTags[0]]

    // 依次求交集
    for _, tag := range requiredTags[1:] {
        candidates = intersection(candidates, tagIndex[tag])
    }

    return mapToSlice(candidates)
}
```

### 3. 心跳队列（解耦接收与处理）

```go
var heartbeatQueue = make(chan *Heartbeat, 10000)

// Worker心跳接收handler（并发）
func handleHeartbeat(hb *Heartbeat) {
    heartbeatQueue <- hb // 非阻塞入队
}

// 后台批量处理线程
go func() {
    ticker := time.NewTicker(1 * time.Second)
    for range ticker.C {
        batch := drainQueue(heartbeatQueue, 1000)
        processBatch(batch) // 批量更新，一次加锁
    }
}()
```

### 内存占用估算

- **Worker状态**: 500台 × 500字节 ≈ 250KB
- **标签索引**: 10种标签 × 200个Worker/标签 ≈ 10KB
- **心跳队列**: 10000条 × 200字节 ≈ 2MB（峰值）
- **总计**: < 3MB

### 并发控制策略

- **心跳更新**: 写锁（批量更新，降低锁竞争）
- **调度查询**: 读锁（高并发，不阻塞）
- **读写比**: 99%读（调度） vs 1%写（心跳）

---

## 错误处理与边界情况

### 1. 无可用Worker（资源不足）

**场景**: 请求需要GPU，但所有带GPU的Worker都满载或下线

**处理**:
```go
if len(candidates) == 0 {
    return &ScheduleResponse{
        Error: "503 No Available Worker",
        Retry: true,
    }
}
```

**客户端策略**:
- 排队等待（推荐）
- 降级到CPU
- 直接失败

**优化**: 调度器可维护FIFO等待队列，Worker释放容量时自动分配

### 2. Worker心跳丢失

**场景**: 网络抖动导致心跳未到达

**处理流程**:
```
0秒 ───────> 10秒 ───────> 20秒
  │            │             │
正常       标记"可疑"      标记"下线"
           (降优先级)     (移出调度池)
                              │
                         恢复心跳 → 自动上线
```

**避免误判**: Worker同时发给主备，降低单点丢包概率

### 3. 调度后Worker立即挂掉

**场景**: 调度器返回Worker地址，但客户端连接时Worker已宕机

**处理**:
```go
// 客户端侧
err := connectToWorker(workerAddr)
if err != nil {
    // 重新请求调度器（最多重试3次）
    newWorker := scheduler.RequestSchedule(task)
}
```

**保障**: 调度器已通过心跳超时检测到Worker下线，不会再次分配

### 4. 乐观分配的竞态问题

**场景**: 调度器内存显示Worker有空位，但实际已满（心跳延迟）

**处理**:
```go
// Worker侧
func acceptTask(task *Task) error {
    if currentTasks >= maxTasks {
        return errors.New("Worker at capacity")
    }
    // 执行任务...
}

// 客户端收到拒绝后，重新调度
```

**发生概率**: 3秒心跳周期内，< 1%（可接受）

### 5. 主备调度器同时挂掉

**场景**: 机房断电或网络分区

**处理**: 系统不可用（接受CAP权衡，优先保证P和C）

**恢复**:
- 调度器重启后，从Worker心跳重建状态
- 恢复时间: < 20秒（等待心跳上报）

### 6. 网络分区（Worker与调度器隔离）

**场景**: 部分Worker因网络故障无法连接调度器

**处理**:
- 超时检测自动下线（20秒）
- 网络恢复后，心跳自动上线
- 已下线Worker上的运行中任务不受影响（继续执行）

---

## 监控与告警

### 关键指标

**调度器指标**:
- 调度请求QPS
- 调度延迟（P50/P99）
- 调度失败率
- 可用Worker数量

**Worker指标**:
- 心跳成功率
- 任务执行成功率
- 资源使用率（CPU/GPU/内存）

**系统健康**:
- 主备同步延迟
- 心跳队列堆积深度

### 告警规则

| 指标 | 阈值 | 级别 |
|------|------|------|
| 调度失败率 | > 5% | P1 |
| Worker下线比例 | > 10% | P1 |
| 主备同步延迟 | > 5秒 | P2 |
| 心跳队列堆积 | > 5000 | P2 |
| 调度延迟P99 | > 100ms | P3 |

---

## 技术栈建议

### 调度器实现

- **语言**: Go（高并发、低延迟、部署简单）
- **Web框架**: 标准库 `net/http`（零依赖）
- **序列化**: Protocol Buffers（高效）或JSON（简单）

### Worker Agent

- **语言**: Go或Python（根据任务类型选择）
- **任务执行**: 进程隔离（避免相互干扰）

### 通信协议

- **心跳**: UDP（低开销）或HTTP（可靠性更高）
- **调度请求**: HTTP/REST
- **任务下发**: gRPC或HTTP

---

## 部署拓扑

```
                    [负载均衡/VIP]
                          │
              ┌───────────┴───────────┐
              │                       │
        ┌──────────┐            ┌──────────┐
        │  主调度器  │ ←─同步──→ │  备调度器  │
        │ (Master)  │            │ (Standby)│
        └──────────┘            └──────────┘
              │                       │
              └───────────┬───────────┘
                          │ 心跳
              ┌───────────┴───────────┐
              │                       │
         [Worker集群 - 500台]
         ├─ GPU Worker (100台)
         ├─ CPU Worker (300台)
         └─ 混合 Worker (100台)
```

---

## 演进路线

### Phase 1: MVP（最小可行产品）

- 单调度器 + 基础心跳机制
- 简单负载均衡（轮询）
- 资源标签过滤

### Phase 2: 高可用

- 主备调度器 + 故障切换
- 负载率排序算法
- 监控与告警

### Phase 3: 优化

- 标签倒排索引（加速过滤）
- 等待队列（资源不足时）
- 历史数据分析（预测负载）

### Phase 4: 扩展

- 分片调度器（支持万台规模）
- 多数据中心支持
- 优先级调度

---

## 总结

本设计提供了一个**简单、高效、零依赖**的分布式任务调度方案，核心特点：

✅ **中心化调度**: 全局视图优化，保证均匀分配
✅ **高可用**: 主备架构，< 10秒故障切换
✅ **高性能**: 微秒级调度，内存状态表
✅ **零依赖**: 无需Redis/Kafka/Zookeeper
✅ **可扩展**: 架构可演进，支持数千台机器

适用于音频处理、视频转码、AI推理等大规模批处理场景。
