package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/hibiken/asynq"
)

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
