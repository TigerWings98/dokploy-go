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
	"github.com/dokploy/dokploy/internal/email"
	"github.com/dokploy/dokploy/internal/notify"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/robfig/cron/v3"
)

// Scheduler manages cron-based scheduled tasks.
type Scheduler struct {
	cron     *cron.Cron
	db       *db.DB
	cfg      *config.Config
	notifier *notify.Notifier
	mu       sync.Mutex
	jobs     map[string]cron.EntryID
}

// New creates a new Scheduler.
func New(database *db.DB, cfg *config.Config, notifier *notify.Notifier) *Scheduler {
	return &Scheduler{
		cron:     cron.New(),
		db:       database,
		cfg:      cfg,
		notifier: notifier,
		jobs:     make(map[string]cron.EntryID),
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
		log.Printf("[Scheduler] Schedule %s (%s) is disabled, skipping", schedule.Name, schedule.ScheduleID)
		return nil
	}

	// 构建 cron 表达式：如果 schedule 指定了时区，使用 CRON_TZ= 前缀
	// 与 TS 版 node-schedule 的 tz 参数行为一致
	// TS 版默认使用 UTC：tz = timezone || "UTC"
	cronExpr := schedule.CronExpression
	if schedule.Timezone != nil && *schedule.Timezone != "" {
		cronExpr = fmt.Sprintf("CRON_TZ=%s %s", *schedule.Timezone, schedule.CronExpression)
	}

	sched := schedule // capture for closure
	entryID, err := s.cron.AddFunc(cronExpr, func() {
		s.executeSchedule(sched)
	})
	if err != nil {
		log.Printf("[Scheduler] Failed to add schedule %s (%s) with cron %q: %v", schedule.Name, schedule.ScheduleID, cronExpr, err)
		return fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}

	s.jobs[schedule.ScheduleID] = entryID
	log.Printf("[Scheduler] Added schedule %s (%s) with cron %q, entryID=%d", schedule.Name, schedule.ScheduleID, cronExpr, entryID)
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
	// 需要 Preload Application/Compose 及其 Server，用于判断是否远程执行
	var freshSchedule schema.Schedule
	if err := s.db.
		Preload("Application").
		Preload("Application.Server").Preload("Application.Server.SSHKey").
		Preload("Compose").
		Preload("Compose.Server").Preload("Compose.Server.SSHKey").
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
// 与 TS 版 runCommand 中 application/compose 分支一致：
// - Application：通过 Docker Swarm label 查找容器（com.docker.swarm.service.name）
// - Compose：通过 Docker Compose label 查找容器（com.docker.compose.project + service）
// - 远程：通过 Application/Compose 的 Server SSH 执行
func (s *Scheduler) executeContainerCommand(schedule schema.Schedule, writeLog func(string), deployment *schema.Deployment) error {
	var appName string
	var server *schema.Server
	var serviceName string

	if schedule.ScheduleType == "application" && schedule.Application != nil {
		appName = schedule.Application.AppName
		// 远程服务器信息从 Application 获取（与 TS 版一致：application.serverId）
		if schedule.Application.ServerID != nil && schedule.Application.Server != nil && schedule.Application.Server.SSHKey != nil {
			server = schedule.Application.Server
		}
	} else if schedule.ScheduleType == "compose" && schedule.Compose != nil {
		appName = schedule.Compose.AppName
		// 远程服务器信息从 Compose 获取（与 TS 版一致：compose.serverId）
		if schedule.Compose.ServerID != nil && schedule.Compose.Server != nil && schedule.Compose.Server.SSHKey != nil {
			server = schedule.Compose.Server
		}
		// compose 类型需要 serviceName 来定位具体容器（与 TS 版 getComposeContainer 一致）
		if schedule.ServiceName != nil {
			serviceName = *schedule.ServiceName
		}
	}

	if appName == "" {
		return fmt.Errorf("no application or compose found for schedule")
	}

	// 构建容器查找命令（与 TS 版 getServiceContainer / getComposeContainer 一致）
	var getContainerCmd string
	if schedule.ScheduleType == "compose" && serviceName != "" {
		// Compose：使用 Docker Compose label 过滤（与 TS 版 getComposeContainer 一致）
		if schedule.Compose != nil && schedule.Compose.ComposeType == "stack" {
			// Stack：使用 swarm label
			getContainerCmd = fmt.Sprintf(
				`docker ps -q --filter "label=com.docker.stack.namespace=%s" --filter "label=com.docker.swarm.service.name=%s_%s" --filter "status=running" | head -1`,
				appName, appName, serviceName,
			)
		} else {
			// Docker Compose：使用 compose label
			getContainerCmd = fmt.Sprintf(
				`docker ps -q --filter "label=com.docker.compose.project=%s" --filter "label=com.docker.compose.service=%s" --filter "status=running" | head -1`,
				appName, serviceName,
			)
		}
	} else {
		// Application：使用 Swarm service label（与 TS 版 getServiceContainer 一致）
		getContainerCmd = fmt.Sprintf(
			`docker ps -q --filter "label=com.docker.swarm.service.name=%s" --filter "status=running" | head -1`,
			appName,
		)
	}

	if server != nil {
		// 远程执行（与 TS 版一致：通过 Application/Compose 的 Server 执行）
		conn := process.SSHConnection{
			Host:       server.IPAddress,
			Port:       server.Port,
			Username:   server.Username,
			PrivateKey: server.SSHKey.PrivateKey,
		}
		result, err := process.ExecAsyncRemote(conn, getContainerCmd, nil)
		if err != nil || result == nil || strings.TrimSpace(result.Stdout) == "" {
			return fmt.Errorf("failed to get container ID for %s on remote: %v", appName, err)
		}
		containerID := strings.TrimSpace(result.Stdout)

		// 与 TS 版一致：在远程 docker exec，输出重定向到日志文件
		execCmd := fmt.Sprintf(`set -e
echo "Running command: docker exec %s %s -c '%s'" >> %s;
docker exec %s %s -c '%s' >> %s 2>> %s || {
	echo "❌ Command failed" >> %s;
	exit 1;
}
echo "✅ Command executed successfully" >> %s;`,
			containerID, schedule.ShellType, schedule.Command, deployment.LogPath,
			containerID, schedule.ShellType, schedule.Command, deployment.LogPath, deployment.LogPath,
			deployment.LogPath,
			deployment.LogPath,
		)
		_, err = process.ExecAsyncRemote(conn, execCmd, writeLog)
		return err
	}

	// 本地执行（与 TS 版一致：spawnAsync docker exec）
	result, err := process.ExecAsync(getContainerCmd)
	if err != nil || result == nil || strings.TrimSpace(result.Stdout) == "" {
		return fmt.Errorf("failed to get container ID for %s: %v", appName, err)
	}
	containerID := strings.TrimSpace(result.Stdout)

	writeLog(fmt.Sprintf("docker exec %s %s -c %s", containerID, schedule.ShellType, schedule.Command))
	_, err = process.ExecAsyncStream(
		fmt.Sprintf("docker exec %s %s -c '%s'", containerID, schedule.ShellType, schedule.Command),
		writeLog,
		process.WithTimeout(30*time.Minute),
	)
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
// TS 版流程：
// 1. 在远程服务器的 /etc/dokploy/schedules/{appName}/script.sh 执行
// 2. 输出通过 tee 同时写入远程日志文件和流式返回
func (s *Scheduler) executeRemoteServerCommand(schedule schema.Schedule, writeLog func(string), deployment *schema.Deployment) error {
	if schedule.Server == nil || schedule.Server.SSHKey == nil {
		return fmt.Errorf("server or SSH key not found for schedule")
	}

	// 与 TS 版 paths(true) 一致：远程服务器上也使用 /etc/dokploy
	schedulesPath := filepath.Join(s.cfg.Paths.BasePath, "schedules")
	fullPath := filepath.Join(schedulesPath, schedule.AppName)

	conn := process.SSHConnection{
		Host:       schedule.Server.IPAddress,
		Port:       schedule.Server.Port,
		Username:   schedule.Server.Username,
		PrivateKey: schedule.Server.SSHKey.PrivateKey,
	}

	// 与 TS 版一致：先在远程创建日志目录和初始化日志文件
	initCmd := fmt.Sprintf(`mkdir -p %s; echo "Initializing schedule" >> %s;`,
		filepath.Join(schedulesPath, schedule.AppName), deployment.LogPath)
	process.ExecAsyncRemote(conn, initCmd, nil)

	// 与 TS 版一致：使用 bash -c 执行 script.sh，输出 tee 到日志文件
	cmd := fmt.Sprintf(`set -e
echo "Running script" >> %s;
bash -c %s/script.sh 2>&1 | tee -a %s || {
	echo "❌ Command failed" >> %s;
	exit 1;
}
echo "✅ Command executed successfully" >> %s;`,
		deployment.LogPath, fullPath, deployment.LogPath, deployment.LogPath, deployment.LogPath)
	writeLog(fmt.Sprintf("Running remote script: %s/script.sh", fullPath))
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
			// 发送 Docker 清理通知给管理员组织
			s.sendDockerCleanupNotification("")
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
		orgID := srv.OrganizationID
		s.AddFunc("docker-cleanup-"+serverID, cleanupCron, func() {
			log.Printf("[Docker Cleanup] Running for server %s", serverID)
			for _, cmd := range cleanupCmds {
				if _, err := process.ExecAsyncRemote(conn, cmd, nil); err != nil {
					log.Printf("[Docker Cleanup] Remote exec failed on %s: %v", serverID, err)
				}
			}
			// 发送 Docker 清理通知
			s.sendDockerCleanupNotification(orgID)
		})
		log.Printf("Docker cleanup cron registered for server %s", serverID)
	}
}

// sendDockerCleanupNotification 发送 Docker 清理完成通知
// orgID 为空时，查询管理员用户的 organizationId
func (s *Scheduler) sendDockerCleanupNotification(orgID string) {
	if s.notifier == nil {
		return
	}
	if orgID == "" {
		// 本地服务器清理：查询 owner 角色成员的组织 ID
		var member schema.Member
		if err := s.db.Where("role = ?", "owner").First(&member).Error; err != nil {
			log.Printf("[Docker Cleanup] Failed to find admin org for notification: %v", err)
			return
		}
		orgID = member.OrganizationID
	}
	htmlBody, _ := email.RenderDockerCleanup(email.DockerCleanupData{})
	s.notifier.Send(orgID, notify.NotificationPayload{
		Event:    notify.EventDockerCleanup,
		Title:    "Docker Cleanup Completed",
		Message:  "Docker cleanup has been completed successfully",
		HTMLBody: htmlBody,
	})
}
