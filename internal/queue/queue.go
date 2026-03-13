// Input: asynq (Redis 任务队列), 部署任务负载 (applicationId/composeId 等)
// Output: Queue (Enqueue/StartWorker), 支持 application/compose/database 部署任务
// Role: 异步任务队列，通过 Redis + asynq 调度部署任务，解耦 HTTP 请求与耗时部署操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/hibiken/asynq"
)

// IsRedisAvailable checks if Redis is reachable and responds to PING.
func IsRedisAvailable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	// Send Redis PING command and check for +PONG response
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
	if err != nil {
		return false
	}
	buf := make([]byte, 32)
	n, err := conn.Read(buf)
	if err != nil {
		return false
	}
	return n >= 5 && string(buf[:5]) == "+PONG"
}

// Task types
const (
	TaskDeployApplication = "deploy:application"
	TaskDeployCompose     = "deploy:compose"
	TaskDeployDatabase    = "deploy:database"
	TaskRebuildDatabase   = "rebuild:database"
	TaskRebuildApplication = "rebuild:application"
	TaskRebuildCompose     = "rebuild:compose"
	TaskStopApplication    = "stop:application"
	TaskStartApplication   = "start:application"
	TaskStopCompose        = "stop:compose"
	TaskStopDatabase       = "stop:database"
	TaskBackupRun         = "backup:run"
	TaskDockerCleanup     = "docker:cleanup"
)

// DeployApplicationPayload is the payload for application deployment tasks.
type DeployApplicationPayload struct {
	ApplicationID string  `json:"applicationId"`
	Title         *string `json:"title,omitempty"`
	Description   *string `json:"description,omitempty"`
}

// DeployComposePayload is the payload for compose deployment tasks.
type DeployComposePayload struct {
	ComposeID string  `json:"composeId"`
	Title     *string `json:"title,omitempty"`
}

// DeployDatabasePayload is the payload for database deployment tasks.
type DeployDatabasePayload struct {
	DatabaseID string `json:"databaseId"`
	Type       string `json:"type"` // postgres, mysql, mariadb, mongo, redis
}

// SimpleIDPayload is a payload with just an ID.
type SimpleIDPayload struct {
	ID string `json:"id"`
}

// Queue manages the deployment task queue.
type Queue struct {
	client    *asynq.Client
	inspector *asynq.Inspector
}

// NewQueue creates a new task queue client.
func NewQueue(redisAddr string) *Queue {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: redisAddr})
	return &Queue{client: client, inspector: inspector}
}

// Close closes the queue client.
func (q *Queue) Close() error {
	return q.client.Close()
}

// QueueJob represents a job in the queue for API responses.
type QueueJob struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	State     string `json:"state"`
	Queue     string `json:"queue"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// ListQueueJobs returns active and pending tasks across all queues.
func (q *Queue) ListQueueJobs() []QueueJob {
	var jobs []QueueJob
	for _, queueName := range []string{"deployments", "backups", "maintenance"} {
		for _, state := range []string{"active", "pending"} {
			var tasks []*asynq.TaskInfo
			var err error
			switch state {
			case "active":
				tasks, err = q.inspector.ListActiveTasks(queueName)
			case "pending":
				tasks, err = q.inspector.ListPendingTasks(queueName)
			}
			if err != nil {
				continue
			}
			for _, t := range tasks {
				jobs = append(jobs, QueueJob{
					ID:      t.ID,
					Type:    t.Type,
					Payload: string(t.Payload),
					State:   state,
					Queue:   queueName,
				})
			}
		}
	}
	if jobs == nil {
		jobs = []QueueJob{}
	}
	return jobs
}

// EnqueueDeployApplication enqueues an application deployment.
func (q *Queue) EnqueueDeployApplication(appID string, title, description *string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(DeployApplicationPayload{
		ApplicationID: appID,
		Title:         title,
		Description:   description,
	})
	task := asynq.NewTask(TaskDeployApplication, payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueRebuildApplication enqueues an application rebuild (build without clone).
func (q *Queue) EnqueueRebuildApplication(appID string, title, description *string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(DeployApplicationPayload{
		ApplicationID: appID,
		Title:         title,
		Description:   description,
	})
	task := asynq.NewTask(TaskRebuildApplication, payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueDeployCompose enqueues a compose deployment.
func (q *Queue) EnqueueDeployCompose(composeID string, title *string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(DeployComposePayload{
		ComposeID: composeID,
		Title:     title,
	})
	task := asynq.NewTask(TaskDeployCompose, payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueRebuildCompose enqueues a compose rebuild (compose up without clone).
func (q *Queue) EnqueueRebuildCompose(composeID string, title *string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(DeployComposePayload{
		ComposeID: composeID,
		Title:     title,
	})
	task := asynq.NewTask(TaskRebuildCompose, payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueDeployDatabase enqueues a database deployment.
func (q *Queue) EnqueueDeployDatabase(databaseID, dbType string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(DeployDatabasePayload{
		DatabaseID: databaseID,
		Type:       dbType,
	})
	task := asynq.NewTask(TaskDeployDatabase, payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueRebuildDatabase enqueues a database rebuild.
func (q *Queue) EnqueueRebuildDatabase(databaseID, dbType string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(DeployDatabasePayload{
		DatabaseID: databaseID,
		Type:       dbType,
	})
	task := asynq.NewTask(TaskRebuildDatabase, payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueStopApplication enqueues an application stop.
func (q *Queue) EnqueueStopApplication(appID string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(SimpleIDPayload{ID: appID})
	task := asynq.NewTask(TaskStopApplication, payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueStartApplication enqueues an application start.
func (q *Queue) EnqueueStartApplication(appID string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(SimpleIDPayload{ID: appID})
	task := asynq.NewTask(TaskStartApplication, payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueStopCompose enqueues a compose stop.
func (q *Queue) EnqueueStopCompose(composeID string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(SimpleIDPayload{ID: composeID})
	task := asynq.NewTask(TaskStopCompose, payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueStopDatabase enqueues a database stop.
func (q *Queue) EnqueueStopDatabase(databaseID, dbType string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(DeployDatabasePayload{
		DatabaseID: databaseID,
		Type:       dbType,
	})
	task := asynq.NewTask(TaskStopDatabase, payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueBackupRun enqueues a backup run.
func (q *Queue) EnqueueBackupRun(backupID string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(SimpleIDPayload{ID: backupID})
	task := asynq.NewTask(TaskBackupRun, payload)
	return q.client.Enqueue(task, asynq.Queue("backups"), asynq.MaxRetry(1))
}

// CancelAllJobs 取消所有队列中的待处理和排队任务
func (q *Queue) CancelAllJobs() {
	for _, queueName := range []string{"deployments", "backups", "maintenance"} {
		// 取消 pending 任务
		if tasks, err := q.inspector.ListPendingTasks(queueName); err == nil {
			for _, t := range tasks {
				_ = q.inspector.DeleteTask(queueName, t.ID)
			}
		}
		// 取消 scheduled 任务
		if tasks, err := q.inspector.ListScheduledTasks(queueName); err == nil {
			for _, t := range tasks {
				_ = q.inspector.DeleteTask(queueName, t.ID)
			}
		}
		// 取消 retry 任务
		if tasks, err := q.inspector.ListRetryTasks(queueName); err == nil {
			for _, t := range tasks {
				_ = q.inspector.DeleteTask(queueName, t.ID)
			}
		}
	}
}

// CancelJobsByFilter 取消匹配过滤条件的任务（用于按 applicationId/composeId 清理）
func (q *Queue) CancelJobsByFilter(filterKey, filterValue string) int {
	deleted := 0
	for _, queueName := range []string{"deployments", "backups", "maintenance"} {
		for _, listFn := range []func(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error){
			q.inspector.ListPendingTasks,
			q.inspector.ListScheduledTasks,
			q.inspector.ListRetryTasks,
		} {
			if tasks, err := listFn(queueName); err == nil {
				for _, t := range tasks {
					var payload map[string]interface{}
					if json.Unmarshal(t.Payload, &payload) == nil {
						if v, ok := payload[filterKey].(string); ok && v == filterValue {
							if q.inspector.DeleteTask(queueName, t.ID) == nil {
								deleted++
							}
						}
					}
				}
			}
		}
	}
	return deleted
}

// EnqueueDockerCleanup enqueues a Docker cleanup task.
func (q *Queue) EnqueueDockerCleanup() (*asynq.TaskInfo, error) {
	task := asynq.NewTask(TaskDockerCleanup, nil)
	return q.client.Enqueue(task, asynq.Queue("maintenance"), asynq.MaxRetry(1))
}

// Worker processes queued tasks.
type Worker struct {
	server *asynq.Server
	mux    *asynq.ServeMux
}

// TaskHandlers holds the service dependencies for task handlers.
type TaskHandlers struct {
	HandleDeployApplication  func(ctx context.Context, payload DeployApplicationPayload) error
	HandleRebuildApplication func(ctx context.Context, payload DeployApplicationPayload) error
	HandleDeployCompose      func(ctx context.Context, payload DeployComposePayload) error
	HandleRebuildCompose     func(ctx context.Context, payload DeployComposePayload) error
	HandleDeployDatabase     func(ctx context.Context, payload DeployDatabasePayload) error
	HandleRebuildDatabase    func(ctx context.Context, payload DeployDatabasePayload) error
	HandleStopCompose       func(ctx context.Context, payload SimpleIDPayload) error
	HandleStopApplication   func(ctx context.Context, payload SimpleIDPayload) error
	HandleStopDatabase      func(ctx context.Context, payload DeployDatabasePayload) error
	HandleStartApplication  func(ctx context.Context, payload SimpleIDPayload) error
	HandleBackupRun         func(ctx context.Context, payload SimpleIDPayload) error
	HandleDockerCleanup     func(ctx context.Context) error
}

// NewWorker creates a new task worker.
func NewWorker(redisAddr string, concurrency int, handlers TaskHandlers) *Worker {
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency: concurrency,
			Queues: map[string]int{
				"deployments":  6,
				"backups":      2,
				"maintenance":  1,
			},
		},
	)

	mux := asynq.NewServeMux()

	// Register handlers
	mux.HandleFunc(TaskDeployApplication, makeHandler(handlers.HandleDeployApplication))
	mux.HandleFunc(TaskRebuildApplication, makeHandler(handlers.HandleRebuildApplication))
	mux.HandleFunc(TaskDeployCompose, makeHandler(handlers.HandleDeployCompose))
	mux.HandleFunc(TaskRebuildCompose, makeHandler(handlers.HandleRebuildCompose))
	mux.HandleFunc(TaskDeployDatabase, makeHandler(handlers.HandleDeployDatabase))
	mux.HandleFunc(TaskRebuildDatabase, makeHandler(handlers.HandleRebuildDatabase))
	mux.HandleFunc(TaskStopCompose, makeHandler(handlers.HandleStopCompose))
	mux.HandleFunc(TaskStopApplication, makeHandler(handlers.HandleStopApplication))
	mux.HandleFunc(TaskStopDatabase, makeHandler(handlers.HandleStopDatabase))
	mux.HandleFunc(TaskStartApplication, makeHandler(handlers.HandleStartApplication))
	mux.HandleFunc(TaskBackupRun, makeHandler(handlers.HandleBackupRun))
	mux.HandleFunc(TaskDockerCleanup, func(ctx context.Context, t *asynq.Task) error {
		if handlers.HandleDockerCleanup != nil {
			return handlers.HandleDockerCleanup(ctx)
		}
		return nil
	})

	return &Worker{server: srv, mux: mux}
}

// Start starts the worker.
func (w *Worker) Start() error {
	log.Println("Starting task worker...")
	return w.server.Start(w.mux)
}

// Stop stops the worker.
func (w *Worker) Stop() {
	w.server.Stop()
}

func makeHandler[T any](handler func(ctx context.Context, payload T) error) func(ctx context.Context, t *asynq.Task) error {
	return func(ctx context.Context, t *asynq.Task) error {
		if handler == nil {
			return fmt.Errorf("handler not registered for task type %s", t.Type())
		}
		var payload T
		if err := json.Unmarshal(t.Payload(), &payload); err != nil {
			return fmt.Errorf("failed to unmarshal payload: %w", err)
		}
		return handler(ctx, payload)
	}
}
