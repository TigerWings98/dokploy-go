# queue

基于 asynq (Redis) 的异步部署任务队列。
替代原 TS 版的 BullMQ，负责部署/重部署任务的序列化入队和 Worker 消费执行。

## 资产清单

| 文件 | 输入/输出 | 核心逻辑 |
|------|----------|----------|
| queue.go | 输入: 部署任务参数 / 输出: 任务执行结果 | asynq 客户端 + Worker：任务入队(Enqueue)、Worker 处理(HandleDeployment)、并发控制、超时/取消处理 |

> 自指声明：一旦本目录下的逻辑发生变化，必须立即同步更新本 README。
