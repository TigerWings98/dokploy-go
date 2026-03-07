package scheduler

import (
	"fmt"
	"log"
	"sync"
	"time"

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
