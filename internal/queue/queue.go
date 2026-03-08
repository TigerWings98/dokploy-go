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
	TaskStopApplication   = "stop:application"
	TaskStartApplication  = "start:application"
	TaskStopCompose       = "stop:compose"
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

// EnqueueDeployCompose enqueues a compose deployment.
func (q *Queue) EnqueueDeployCompose(composeID string, title *string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(DeployComposePayload{
		ComposeID: composeID,
		Title:     title,
	})
	task := asynq.NewTask(TaskDeployCompose, payload)
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
	task := asynq.NewTask("stop:database", payload)
	return q.client.Enqueue(task, asynq.Queue("deployments"), asynq.MaxRetry(0))
}

// EnqueueBackupRun enqueues a backup run.
func (q *Queue) EnqueueBackupRun(backupID string) (*asynq.TaskInfo, error) {
	payload, _ := json.Marshal(SimpleIDPayload{ID: backupID})
	task := asynq.NewTask(TaskBackupRun, payload)
	return q.client.Enqueue(task, asynq.Queue("backups"), asynq.MaxRetry(1))
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
	HandleDeployApplication func(ctx context.Context, payload DeployApplicationPayload) error
	HandleDeployCompose     func(ctx context.Context, payload DeployComposePayload) error
	HandleDeployDatabase    func(ctx context.Context, payload DeployDatabasePayload) error
	HandleRebuildDatabase   func(ctx context.Context, payload DeployDatabasePayload) error
	HandleStopApplication   func(ctx context.Context, payload SimpleIDPayload) error
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
	mux.HandleFunc(TaskDeployCompose, makeHandler(handlers.HandleDeployCompose))
	mux.HandleFunc(TaskDeployDatabase, makeHandler(handlers.HandleDeployDatabase))
	mux.HandleFunc(TaskRebuildDatabase, makeHandler(handlers.HandleRebuildDatabase))
	mux.HandleFunc(TaskStopApplication, makeHandler(handlers.HandleStopApplication))
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
