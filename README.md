# RLock - Redis 分布式锁

## 概述

RLock 是一个使用 Redis 实现的分布式锁库。它允许多个进程或线程在分布式系统中安全地协同工作，以确保一次只有一个执行者可以持有锁。

## 功能

- **分布式锁定**：通过 Redis 实现跨多个节点的锁定机制。
- **可配置的锁定时间**：可以设置最大锁定时间，防止死锁。
- **时钟漂移考虑**：考虑到时钟漂移，增加锁定的稳定性。
- **重试机制**：在无法获得锁时，提供了重试获取锁的机制。
- **随机字符串生成**：为每个锁生成唯一标识，确保锁的唯一性。

## 使用方法

### 创建 RLock 实例

首先，需要创建一个 RLock 实例。可以通过提供一组 Redis 客户端实例来完成。

```go
clients := []*redis.Client{...} // Redis 客户端实例列表
rlock, err := NewRLock(clients)
if err != nil {
    // 处理错误
}
```

### 配置选项

可以通过选项函数来配置 RLock 实例：

   - WithMaxLockTime(maxLockTime time.Duration) 设置最大锁定时间。
   - WithLimit(limit int) 设置并发限制。
   - WithLockID(lockID string) 设置锁的唯一标识。
   - WithClockDrift(drift float64) 设置时钟漂移容忍度。
   - WithRetryTimes(retry int) 设置重试次数。

### 获取锁

```go
ttl := 10 * time.Second // 锁定时间
ctx := context.Background()
leftTTL, err := rlock.Lock(ctx, "my-lock-key", ttl)
if err != nil {
    // 处理错误
}
// 成功获取锁，leftTTL 是剩余的锁定时间
```

### 释放锁

```go
err := rlock.Unlock(ctx, "my-lock-key")
if err != nil {
    // 处理错误
}
// 成功释放锁
```
## 注意事项

确保 Redis 客户端列表的数量为奇数，以避免脑裂问题。
锁定时间不应该过长，以免影响系统性能。
使用分布式锁时，务必确保所有操作都是幂等的。


## 许可

RLock 是在 MIT 许可下发布的。有关详细信息，请查看 LICENSE 文件。