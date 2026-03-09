// Input: robfig/cron, db (Schedule/Backup 表), process (命令执行), docker (清理)
// Output: Scheduler (AddJob/RemoveJob/RunNow), 支持备份/清理/自定义脚本三类定时任务
// Role: Cron 定时任务调度器，管理备份定时任务、Docker 清理任务和自定义脚本执行
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package scheduler

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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

	// 构建 cron 表达式：如果 schedule 指定了时区，使用 CRON_TZ= 前缀
	// 与 TS 版 node-schedule 的 tz 参数行为一致
	// 例如 timezone="Asia/Shanghai", cronExpr="0 3 * * *"
	// → "CRON_TZ=Asia/Shanghai 0 3 * * *"，在上海时间 03:00 执行
	cronExpr := schedule.CronExpression
	if schedule.Timezone != nil && *schedule.Timezone != "" {
		cronExpr = fmt.Sprintf("CRON_TZ=%s %s", *schedule.Timezone, schedule.CronExpression)
	}

	sched := schedule // capture for closure
	entryID, err := s.cron.AddFunc(cronExpr, func() {
		s.executeSchedule(sched)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
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
// 与 TS 版 runCommand + createDeploymentSchedule 行为一致：
// - 创建日志文件写入实时输出
// - Deployment 记录包含 logPath/startedAt/finishedAt
// - title/description 使用 "Schedule"（与 TS 版一致）
func (s *Scheduler) executeSchedule(schedule schema.Schedule) {
	// 重新从数据库读取最新的 schedule（可能已更新命令/配置）
	var freshSchedule schema.Schedule
	if err := s.db.
		Preload("Application").
		Preload("Compose").
		Preload("Server").Preload("Server.SSHKey").
		First(&freshSchedule, "\"scheduleId\" = ?", schedule.ScheduleID).Error; err != nil {
		log.Printf("Schedule %s not found, skipping execution", schedule.ScheduleID)
		return
	}
	schedule = freshSchedule

	log.Printf("Executing schedule %s (%s): %s", schedule.Name, schedule.ScheduleID, schedule.Command)

	// 清理旧的部署记录（保留最近10条，与 TS 版 removeLastTenDeployments 一致）
	s.removeOldDeployments(schedule.ScheduleID)

	// 构建日志文件路径（与 TS 版 createDeploymentSchedule 一致）
	// TS: path.join(SCHEDULES_PATH, schedule.appName, `${schedule.appName}-${formattedDateTime}.log`)
	schedulesPath := filepath.Join(s.cfg.Paths.BasePath, "schedules")
	formattedDateTime := time.Now().UTC().Format("2006-01-02:15:04:05")
	logFileName := fmt.Sprintf("%s-%s.log", schedule.AppName, formattedDateTime)
	logDir := filepath.Join(schedulesPath, schedule.AppName)
	logFilePath := filepath.Join(logDir, logFileName)

	// 创建日志目录和初始化日志文件
	os.MkdirAll(logDir, 0755)
	os.WriteFile(logFilePath, []byte("Initializing schedule\n"), 0644)

	// 创建 deployment 记录（与 TS 版格式完全一致）
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statusRunning := schema.DeploymentStatusRunning
	desc := "Schedule"
	deployment := &schema.Deployment{
		Title:       "Schedule",
		Description: &desc,
		Status:      &statusRunning,
		LogPath:     logFilePath,
		ScheduleID:  &schedule.ScheduleID,
		StartedAt:   &now,
	}
	if schedule.ApplicationID != nil {
		deployment.ApplicationID = schedule.ApplicationID
	}
	if schedule.ComposeID != nil {
		deployment.ComposeID = schedule.ComposeID
	}
	s.db.Create(deployment)

	// 打开日志文件用于追加写入
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Failed to open log file: %v", err)
		s.finishDeployment(deployment.DeploymentID, schema.DeploymentStatusError)
		return
	}
	defer logFile.Close()

	writeLog := func(msg string) {
		logFile.WriteString(msg + "\n")
	}

	// 根据 scheduleType 执行命令（与 TS 版一致）
	var execErr error
	switch schedule.ScheduleType {
	case "application", "compose":
		execErr = s.executeContainerCommand(schedule, writeLog, deployment)
	case "dokploy-server":
		execErr = s.executeDokployServerCommand(schedule, writeLog, deployment)
	case "server":
		execErr = s.executeRemoteServerCommand(schedule, writeLog, deployment)
	default:
		// 直接执行命令
		writeLog(fmt.Sprintf("Running command: %s", schedule.Command))
		_, execErr = process.ExecAsyncStream(schedule.Command, writeLog,
			process.WithTimeout(30*time.Minute),
		)
	}

	if execErr != nil {
		writeLog(fmt.Sprintf("❌ Command failed: %v", execErr))
		log.Printf("Schedule %s failed: %v", schedule.ScheduleID, execErr)
		s.finishDeployment(deployment.DeploymentID, schema.DeploymentStatusError)
		return
	}

	writeLog("✅ Command executed successfully")
	s.finishDeployment(deployment.DeploymentID, schema.DeploymentStatusDone)
}

// executeContainerCommand 在应用/compose 容器内执行命令（docker exec）
func (s *Scheduler) executeContainerCommand(schedule schema.Schedule, writeLog func(string), deployment *schema.Deployment) error {
	var appName string
	var serverID *string

	if schedule.ScheduleType == "application" && schedule.Application != nil {
		appName = schedule.Application.AppName
		serverID = schedule.Application.ServerID
	} else if schedule.ScheduleType == "compose" && schedule.Compose != nil {
		appName = schedule.Compose.AppName
		serverID = schedule.Compose.ServerID
	}

	if appName == "" {
		return fmt.Errorf("no application or compose found for schedule")
	}

	// 获取容器 ID（与 TS 版 getServiceContainer 一致）
	getContainerCmd := fmt.Sprintf("docker ps -q -f name=%s | head -1", appName)

	if serverID != nil && schedule.Server != nil && schedule.Server.SSHKey != nil {
		conn := process.SSHConnection{
			Host:       schedule.Server.IPAddress,
			Port:       schedule.Server.Port,
			Username:   schedule.Server.Username,
			PrivateKey: schedule.Server.SSHKey.PrivateKey,
		}
		result, err := process.ExecAsyncRemote(conn, getContainerCmd, nil)
		if err != nil || result == nil || result.Stdout == "" {
			return fmt.Errorf("failed to get container ID: %v", err)
		}
		containerID := strings.TrimSpace(result.Stdout)

		execCmd := fmt.Sprintf("docker exec %s %s -c '%s'", containerID, schedule.ShellType, schedule.Command)
		writeLog(fmt.Sprintf("Running command: %s", execCmd))
		_, err = process.ExecAsyncRemote(conn, execCmd, writeLog)
		return err
	}

	// 本地执行
	result, err := process.ExecAsync(getContainerCmd)
	if err != nil || result == nil || result.Stdout == "" {
		return fmt.Errorf("failed to get container ID for %s", appName)
	}
	containerID := strings.TrimSpace(result.Stdout)

	execCmd := fmt.Sprintf("docker exec %s %s -c '%s'", containerID, schedule.ShellType, schedule.Command)
	writeLog(fmt.Sprintf("Running command: %s", execCmd))
	_, err = process.ExecAsyncStream(execCmd, writeLog, process.WithTimeout(30*time.Minute))
	return err
}

// executeDokployServerCommand 在本地服务器执行 script.sh（与 TS 版一致）
func (s *Scheduler) executeDokployServerCommand(schedule schema.Schedule, writeLog func(string), deployment *schema.Deployment) error {
	schedulesPath := filepath.Join(s.cfg.Paths.BasePath, "schedules")
	fullPath := filepath.Join(schedulesPath, schedule.AppName)

	writeLog(fmt.Sprintf("Running script at %s/script.sh", fullPath))
	_, err := process.ExecAsyncStream("bash -c ./script.sh", func(data string) {
		writeLog(data)
		if pid := extractPID(data); pid != "" {
			s.db.Model(&schema.Deployment{}).
				Where("\"deploymentId\" = ?", deployment.DeploymentID).
				Update("pid", pid)
		}
	}, process.WithDir(fullPath), process.WithTimeout(30*time.Minute))
	return err
}

// executeRemoteServerCommand 在远程服务器执行 script.sh（与 TS 版一致）
func (s *Scheduler) executeRemoteServerCommand(schedule schema.Schedule, writeLog func(string), deployment *schema.Deployment) error {
	if schedule.Server == nil || schedule.Server.SSHKey == nil {
		return fmt.Errorf("server or SSH key not found for schedule")
	}

	schedulesPath := filepath.Join(s.cfg.Paths.BasePath, "schedules")
	fullPath := filepath.Join(schedulesPath, schedule.AppName)

	conn := process.SSHConnection{
		Host:       schedule.Server.IPAddress,
		Port:       schedule.Server.Port,
		Username:   schedule.Server.Username,
		PrivateKey: schedule.Server.SSHKey.PrivateKey,
	}

	cmd := fmt.Sprintf("bash -c %s/script.sh", fullPath)
	writeLog(fmt.Sprintf("Running remote script: %s", cmd))
	_, err := process.ExecAsyncRemote(conn, cmd, func(data string) {
		writeLog(data)
		if pid := extractPID(data); pid != "" {
			s.db.Model(&schema.Deployment{}).
				Where("\"deploymentId\" = ?", deployment.DeploymentID).
				Update("pid", pid)
		}
	})
	return err
}

// extractPID 从输出中提取 PID（与 TS 版正则 /PID: (\d+)/ 一致）
func extractPID(data string) string {
	idx := strings.Index(data, "PID: ")
	if idx < 0 {
		return ""
	}
	start := idx + 5
	end := start
	for end < len(data) && data[end] >= '0' && data[end] <= '9' {
		end++
	}
	if end > start {
		return data[start:end]
	}
	return ""
}

// finishDeployment 更新 deployment 的最终状态和完成时间
func (s *Scheduler) finishDeployment(deploymentID string, status schema.DeploymentStatus) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.db.Model(&schema.Deployment{}).
		Where("\"deploymentId\" = ?", deploymentID).
		Updates(map[string]interface{}{
			"status":     status,
			"finishedAt": now,
		})
}

// removeOldDeployments 保留最近10条 schedule 部署记录，删除旧的（与 TS v0.28.5 一致）
// 每条记录独立 try-catch，防止单条删除失败阻塞其他清理
func (s *Scheduler) removeOldDeployments(scheduleID string) {
	var deployments []schema.Deployment
	s.db.Where("\"scheduleId\" = ?", scheduleID).
		Order("\"createdAt\" DESC").
		Offset(10).
		Find(&deployments)
	for _, d := range deployments {
		// 路径验证：防止误删根目录或空路径（与 TS v0.28.5 对齐）
		if d.LogPath != "" && d.LogPath != "." {
			if err := os.Remove(d.LogPath); err != nil && !os.IsNotExist(err) {
				log.Printf("Warning: failed to remove log file %s: %v", d.LogPath, err)
			}
		}
		if err := s.db.Delete(&d).Error; err != nil {
			log.Printf("Warning: failed to remove old deployment %s: %v", d.DeploymentID, err)
		}
	}
}

// RunNow executes a schedule immediately (bypassing cron).
func (s *Scheduler) RunNow(schedule schema.Schedule) {
	go s.executeSchedule(schedule)
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
