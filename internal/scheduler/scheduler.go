// Input: robfig/cron, db (Schedule/Backup 表), process (命令执行), docker (清理)
// Output: Scheduler (AddJob/RemoveJob/RunNow), 支持备份/清理/自定义脚本三类定时任务
// Role: Cron 定时任务调度器，管理备份定时任务、Docker 清理任务和自定义脚本执行
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package scheduler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"os/exec"

	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/robfig/cron/v3"
)

// Scheduler manages cron-based scheduled tasks.
type Scheduler struct {
	cron *cron.Cron
	db   *db.DB
	cfg  *config.Config
	mu   sync.Mutex
	jobs map[string]cron.EntryID
}

// New creates a new Scheduler.
func New(database *db.DB, cfg *config.Config) *Scheduler {
	return &Scheduler{
		cron: cron.New(),
		db:   database,
		cfg:  cfg,
		jobs: make(map[string]cron.EntryID),
	}
}

// InitSchedules loads all enabled schedules and starts the cron runner.
func (s *Scheduler) InitSchedules() {
	var schedules []schema.Schedule
	if err := s.db.Where("enabled = ?", true).Find(&schedules).Error; err != nil {
		log.Printf("Warning: failed to load schedules: %v", err)
		return
	}

	for _, sched := range schedules {
		if err := s.addJob(sched); err != nil {
			log.Printf("Failed to add schedule %s: %v", sched.ScheduleID, err)
		}
	}

	// Restore docker cleanup cron jobs
	s.initDockerCleanup()

	s.cron.Start()
	log.Printf("Scheduler started with %d jobs", len(s.jobs))
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Println("Scheduler stopped")
}

// AddJob adds or updates a scheduled job.
func (s *Scheduler) AddJob(schedule schema.Schedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addJob(schedule)
}

func (s *Scheduler) addJob(schedule schema.Schedule) error {
	// Remove existing job if present
	if entryID, ok := s.jobs[schedule.ScheduleID]; ok {
		s.cron.Remove(entryID)
		delete(s.jobs, schedule.ScheduleID)
	}

	if !schedule.Enabled {
		return nil
	}

	sched := schedule // capture for closure
	entryID, err := s.cron.AddFunc(sched.CronExpression, func() {
		s.executeSchedule(sched)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", schedule.CronExpression, err)
	}

	s.jobs[schedule.ScheduleID] = entryID
	return nil
}

// RemoveJob removes a scheduled job.
func (s *Scheduler) RemoveJob(scheduleID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.jobs[scheduleID]; ok {
		s.cron.Remove(entryID)
		delete(s.jobs, scheduleID)
	}
}

// executeSchedule runs a scheduled command.
func (s *Scheduler) executeSchedule(schedule schema.Schedule) {
	log.Printf("Executing schedule %s (%s): %s", schedule.Name, schedule.ScheduleID, schedule.Command)

	// Create deployment record
	statusRunning := schema.DeploymentStatusRunning
	deployment := &schema.Deployment{
		Title:      schedule.Name,
		Status:     &statusRunning,
		ScheduleID: &schedule.ScheduleID,
	}
	if schedule.ApplicationID != nil {
		deployment.ApplicationID = schedule.ApplicationID
	}
	if schedule.ComposeID != nil {
		deployment.ComposeID = schedule.ComposeID
	}
	s.db.Create(deployment)

	// Execute the command
	result, err := process.ExecAsync(schedule.Command,
		process.WithTimeout(30*time.Minute),
	)

	statusDone := schema.DeploymentStatusDone
	if err != nil {
		statusDone = schema.DeploymentStatusError
		log.Printf("Schedule %s failed: %v", schedule.ScheduleID, err)
	}

	output := ""
	if result != nil {
		output = result.Stdout
	}

	// Update deployment status
	s.db.Model(deployment).Updates(map[string]interface{}{
		"status":      statusDone,
		"logPath":     "",
		"description": output,
	})
}

// RunNow executes a schedule immediately (bypassing cron).
func (s *Scheduler) RunNow(schedule schema.Schedule) {
	s.executeSchedule(schedule)
}

// AddFunc adds a named cron job with a plain function (not tied to schema.Schedule).
func (s *Scheduler) AddFunc(name, cronExpr string, fn func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing job if present
	if entryID, ok := s.jobs[name]; ok {
		s.cron.Remove(entryID)
		delete(s.jobs, name)
	}

	entryID, err := s.cron.AddFunc(cronExpr, fn)
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}
	s.jobs[name] = entryID
	return nil
}

// ReloadSchedule reloads a single schedule from the database.
func (s *Scheduler) ReloadSchedule(scheduleID string) error {
	var schedule schema.Schedule
	if err := s.db.First(&schedule, "\"scheduleId\" = ?", scheduleID).Error; err != nil {
		// Schedule deleted - remove the job
		s.RemoveJob(scheduleID)
		return nil
	}

	return s.AddJob(schedule)
}

// initDockerCleanup restores docker cleanup cron jobs for settings and servers
// that had enableDockerCleanup=true before restart.
func (s *Scheduler) initDockerCleanup() {
	const cleanupCron = "50 23 * * *"
	cleanupCmds := []string{
		"docker container prune --force",
		"docker image prune --all --force",
		"docker builder prune --all --force",
		"docker system prune --all --force",
	}

	// Check main server settings
	var settings schema.WebServerSettings
	if err := s.db.First(&settings).Error; err == nil && settings.EnableDockerCleanup {
		s.AddFunc("docker-cleanup", cleanupCron, func() {
			log.Printf("[Docker Cleanup] Running for local server")
			for _, cmd := range cleanupCmds {
				c := exec.Command("bash", "-c", cmd)
				if output, err := c.CombinedOutput(); err != nil {
					log.Printf("[Docker Cleanup] Local exec failed: %v: %s", err, string(output))
				}
			}
		})
		log.Println("Docker cleanup cron registered for local server")
	}

	// Check remote servers
	var servers []schema.Server
	s.db.Preload("SSHKey").Where("\"enableDockerCleanup\" = ?", true).Find(&servers)
	for _, srv := range servers {
		if srv.SSHKey == nil || srv.ServerStatus == "inactive" {
			continue
		}
		serverID := srv.ServerID
		conn := process.SSHConnection{
			Host:       srv.IPAddress,
			Port:       srv.Port,
			Username:   srv.Username,
			PrivateKey: srv.SSHKey.PrivateKey,
		}
		s.AddFunc("docker-cleanup-"+serverID, cleanupCron, func() {
			log.Printf("[Docker Cleanup] Running for server %s", serverID)
			for _, cmd := range cleanupCmds {
				if _, err := process.ExecAsyncRemote(conn, cmd, nil); err != nil {
					log.Printf("[Docker Cleanup] Remote exec failed on %s: %v", serverID, err)
				}
			}
		})
		log.Printf("Docker cleanup cron registered for server %s", serverID)
	}
}
