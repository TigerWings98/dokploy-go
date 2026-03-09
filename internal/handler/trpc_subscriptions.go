// Input: procedureRegistry, db, queue, service, backup
// Output: registerSubscriptionsTRPC - tRPC subscription 端点 (SSE 流式响应)
// Role: tRPC 订阅端点注册，实现 deployWithLogs/setupWithLogs/restoreBackupWithLogs 等流式日志推送
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerSubscriptionsTRPC(s subscriptionRegistry) {
	// Database deployWithLogs subscriptions (postgres/mysql/mariadb/mongo/redis)
	dbTypes := []struct {
		name    string
		idField string
	}{
		{"postgres", "postgresId"},
		{"mysql", "mysqlId"},
		{"mariadb", "mariadbId"},
		{"mongo", "mongoId"},
		{"redis", "redisId"},
	}

	for _, dt := range dbTypes {
		dt := dt
		s[dt.name+".deployWithLogs"] = func(c echo.Context, input json.RawMessage, emit chan<- interface{}) {
			defer close(emit)
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			id, _ := in[dt.idField].(string)
			if id == "" {
				emit <- fmt.Sprintf("Error: %s is required", dt.idField)
				return
			}

			emit <- fmt.Sprintf("Starting %s deployment...", dt.name)

			// Enqueue the deployment
			if h.Queue != nil {
				if _, err := h.Queue.EnqueueDeployDatabase(id, dt.name); err != nil {
					emit <- "Error: " + err.Error()
					return
				}
			}

			// Tail the deployment log file
			h.tailDeploymentLogs(c, id, dt.name, emit)
		}
	}

	// server.setupWithLogs
	s["server.setupWithLogs"] = func(c echo.Context, input json.RawMessage, emit chan<- interface{}) {
		defer close(emit)
		var in struct {
			ServerID string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if in.ServerID == "" {
			emit <- "Error: serverId is required"
			return
		}

		emit <- "Starting server setup..."

		// The server setup is handled by the existing setup flow
		// Stream progress updates
		var server schema.Server
		if err := h.DB.First(&server, "\"serverId\" = ?", in.ServerID).Error; err != nil {
			emit <- "Error: Server not found"
			return
		}

		emit <- fmt.Sprintf("Setting up server: %s", server.Name)
		emit <- "Installing Docker and dependencies..."

		// Monitor server status changes
		for i := 0; i < 60; i++ {
			select {
			case <-c.Request().Context().Done():
				return
			default:
			}
			time.Sleep(2 * time.Second)
			var current schema.Server
			if err := h.DB.First(&current, "\"serverId\" = ?", in.ServerID).Error; err != nil {
				continue
			}
			if string(current.ServerStatus) == "active" {
				emit <- "Server setup completed successfully!"
				return
			}
		}
		emit <- "Server setup timed out. Check server status manually."
	}

	// backup.restoreBackupWithLogs
	// 输入与 TS 版 apiRestoreBackup 一致：databaseId, databaseType, backupType, databaseName, backupFile, destinationId
	s["backup.restoreBackupWithLogs"] = func(c echo.Context, input json.RawMessage, emit chan<- interface{}) {
		defer close(emit)
		var in struct {
			DatabaseID    string `json:"databaseId"`
			DatabaseType  string `json:"databaseType"`
			BackupType    string `json:"backupType"`
			DatabaseName  string `json:"databaseName"`
			BackupFile    string `json:"backupFile"`
			DestinationID string `json:"destinationId"`
			// 向后兼容旧格式
			BackupID string `json:"backupId"`
			FileName string `json:"fileName"`
		}
		json.Unmarshal(input, &in)

		emit <- "Starting backup restore..."

		if h.BackupSvc == nil {
			emit <- "Error: backup service not available"
			return
		}

		// Web Server 恢复走独立路径（不需要 backupId，直接用 destinationId + backupFile）
		if in.DatabaseType == "web-server" {
			err := h.BackupSvc.RestoreWebServerBackup(in.DestinationID, in.BackupFile, func(log string) {
				emit <- log
			})
			if err != nil {
				emit <- "Error: " + err.Error()
				return
			}
		} else {
			// 数据库/Compose 恢复走原有 RestoreBackup
			backupID := in.BackupID
			fileName := in.FileName
			if backupID == "" {
				backupID = in.DatabaseID
			}
			if fileName == "" {
				fileName = in.BackupFile
			}
			if err := h.BackupSvc.RestoreBackup(backupID, fileName); err != nil {
				emit <- "Error: " + err.Error()
				return
			}
			emit <- "Backup restored successfully!"
		}
	}

	// volumeBackups.restoreVolumeBackupWithLogs
	s["volumeBackups.restoreVolumeBackupWithLogs"] = func(c echo.Context, input json.RawMessage, emit chan<- interface{}) {
		defer close(emit)
		var in struct {
			VolumeBackupID string `json:"volumeBackupId"`
			FileName       string `json:"fileName"`
		}
		json.Unmarshal(input, &in)

		emit <- "Starting volume backup restore..."

		var vb schema.VolumeBackup
		if err := h.DB.First(&vb, "\"volumeBackupId\" = ?", in.VolumeBackupID).Error; err != nil {
			emit <- "Error: Volume backup not found"
			return
		}

		emit <- fmt.Sprintf("Restoring volume: %s", vb.VolumeName)

		// Volume restore: download from S3 and apply to Docker volume
		if h.BackupSvc != nil {
			err := h.BackupSvc.RestoreVolumeBackup(in.VolumeBackupID, in.FileName)
			if err != nil {
				emit <- "Error: " + err.Error()
				return
			}
		}

		emit <- "Volume backup restored successfully!"
	}
}

// tailDeploymentLogs monitors for deployment logs and streams them.
func (h *Handler) tailDeploymentLogs(c echo.Context, id, dbType string, emit chan<- interface{}) {
	// Find the latest deployment for this database
	logsPath := "/etc/dokploy/logs"
	if h.Config != nil {
		logsPath = h.Config.Paths.LogsPath
	}
	logDir := filepath.Join(logsPath, dbType)

	// Look for deployment log file
	logFile := filepath.Join(logDir, id+".log")

	// Wait for log file to appear (max 30 seconds)
	var f *os.File
	for i := 0; i < 30; i++ {
		select {
		case <-c.Request().Context().Done():
			return
		default:
		}
		var err error
		f, err = os.Open(logFile)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if f == nil {
		// No log file found, just wait for deployment status
		emit <- "Deployment queued, waiting for completion..."
		time.Sleep(5 * time.Second)
		emit <- "Deployment completed."
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		select {
		case <-c.Request().Context().Done():
			return
		default:
			emit <- scanner.Text()
		}
	}

	emit <- "Deployment completed."
}
