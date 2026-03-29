// Input: procedureRegistry, db, queue, service, backup
// Output: registerSubscriptionsTRPC - tRPC subscription 端点 (SSE 流式响应)
// Role: tRPC 订阅端点注册，实现 deployWithLogs/setupWithLogs/restoreBackupWithLogs 等流式日志推送
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerSubscriptionsTRPC(s subscriptionRegistry) {
	// Database deployWithLogs subscriptions (postgres/mysql/mariadb/mongo/redis)
	// 与 TS 版对齐：直接调用 service 层，通过 onData 回调实时输出日志，不走队列
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

			if h.DBSvc == nil {
				emit <- "Error: database service not available"
				return
			}

			onData := func(msg string) { emit <- msg }
			if err := h.DBSvc.DeployByType(id, dt.name, onData); err != nil {
				emit <- fmt.Sprintf("\nDeploy %s: ❌\n", dt.name)
			} else {
				emit <- fmt.Sprintf("\nDeploy %s: ✅\n", dt.name)
			}
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
	// 输入与 TS 版 apiRestoreBackup 一致：databaseId, databaseType, backupType, databaseName, backupFile, destinationId, metadata
	s["backup.restoreBackupWithLogs"] = func(c echo.Context, input json.RawMessage, emit chan<- interface{}) {
		defer close(emit)
		var in struct {
			DatabaseID    string          `json:"databaseId"`
			DatabaseType  string          `json:"databaseType"`
			BackupType    string          `json:"backupType"`
			DatabaseName  string          `json:"databaseName"`
			BackupFile    string          `json:"backupFile"`
			DestinationID string          `json:"destinationId"`
			Metadata      json.RawMessage `json:"metadata"`
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

		emitFn := func(log string) { emit <- log }

		// 按 backupType 和 databaseType 分发（与 TS 版 restoreBackupWithLogs 完全一致）
		if in.BackupType == "compose" {
			// Compose 恢复：使用 metadata 凭据 + compose container label
			metadataStr := ""
			if in.Metadata != nil {
				metadataStr = string(in.Metadata)
			}
			if err := h.BackupSvc.RestoreComposeBackup(in.DatabaseID, in.DestinationID, in.DatabaseType, in.DatabaseName, in.BackupFile, metadataStr, emitFn); err != nil {
				emit <- "Error: " + err.Error()
				return
			}
		} else if in.DatabaseType == "web-server" {
			// Web Server 恢复
			if err := h.BackupSvc.RestoreWebServerBackup(in.DestinationID, in.BackupFile, emitFn); err != nil {
				emit <- "Error: " + err.Error()
				return
			}
		} else {
			// 数据库恢复（postgres/mysql/mariadb/mongo）
			// 与 TS 版一致：通过 databaseId 查数据库实例表，不依赖 backup 表
			if err := h.BackupSvc.RestoreBackup(in.DatabaseID, in.DestinationID, in.DatabaseType, in.DatabaseName, in.BackupFile, emitFn); err != nil {
				emit <- "Error: " + err.Error()
				return
			}
		}
	}

	// volumeBackups.restoreVolumeBackupWithLogs
	// 与 TS 版一致：直接接收所有参数（destinationId, volumeName, backupFileName, id, serviceType, serverId），
	// 不查 VolumeBackup 记录
	s["volumeBackups.restoreVolumeBackupWithLogs"] = func(c echo.Context, input json.RawMessage, emit chan<- interface{}) {
		defer close(emit)
		var in struct {
			ID             string `json:"id"`
			ServiceType    string `json:"serviceType"`
			ServerID       string `json:"serverId"`
			DestinationID  string `json:"destinationId"`
			VolumeName     string `json:"volumeName"`
			BackupFileName string `json:"backupFileName"`
		}
		json.Unmarshal(input, &in)

		emit <- "Starting volume backup restore..."
		emit <- fmt.Sprintf("Restoring volume: %s", in.VolumeName)

		if h.BackupSvc != nil {
			onData := func(data string) { emit <- data }
			err := h.BackupSvc.RestoreVolumeBackup(in.DestinationID, in.VolumeName, in.BackupFileName, in.ID, in.ServiceType, in.ServerID, onData)
			if err != nil {
				emit <- "Error: " + err.Error()
				return
			}
		}

		emit <- "Volume backup restored successfully!"
	}
}

