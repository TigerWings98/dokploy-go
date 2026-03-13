// Input: procedureRegistry, db (Backup/Destination 表), backup, scheduler
// Output: registerBackupTRPC - Backup/Destination/VolumeBackup 领域的 tRPC procedure 注册
// Role: Backup tRPC 路由注册，将 backup/destination/volumeBackup.* procedure 绑定到备份管理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// rcloneFile 与 TS 版 RcloneFile 结构一致，用于 rclone lsjson 输出解析
type rcloneFile struct {
	Path   string            `json:"Path"`
	Name   string            `json:"Name"`
	Size   int64             `json:"Size"`
	IsDir  bool              `json:"IsDir"`
	Hashes map[string]string `json:"Hashes,omitempty"`
}

func (h *Handler) registerBackupTRPC(r procedureRegistry) {
	r["backup.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			BackupID string `json:"backupId"`
		}
		json.Unmarshal(input, &in)
		var b schema.Backup
		if err := h.DB.Preload("Destination").
			Preload("Deployments", func(db *gorm.DB) *gorm.DB {
				return db.Order("\"createdAt\" DESC")
			}).
			First(&b, "\"backupId\" = ?", in.BackupID).Error; err != nil {
			return nil, &trpcErr{"Backup not found", "NOT_FOUND", 404}
		}
		if b.Deployments == nil {
			b.Deployments = []schema.Deployment{}
		}
		return b, nil
	}

	r["backup.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var b schema.Backup
		json.Unmarshal(input, &b)
		// metadata 字段：前端可能发送空对象 {} 或 undefined，需要特殊处理
		// 因为 Metadata 是 *string（存 jsonb），直接 unmarshal {} 到 *string 会失败
		var raw map[string]json.RawMessage
		json.Unmarshal(input, &raw)
		if m, ok := raw["metadata"]; ok && len(m) > 0 && string(m) != "null" {
			s := string(m)
			// 空对象 {} 存为 null（与 TS 版行为一致：metadata 可选，空时不存）
			if s == "{}" {
				b.Metadata = nil
			} else {
				b.Metadata = &s
			}
		}
		if err := h.DB.Create(&b).Error; err != nil {
			return nil, err
		}
		// Schedule backup cron job if enabled
		if h.BackupSvc != nil && b.Enabled != nil && *b.Enabled {
			h.BackupSvc.ScheduleBackup(b)
		}
		// 重新查询以包含 destination 关联（与 TS 版一致）
		h.DB.Preload("Destination").First(&b, "\"backupId\" = ?", b.BackupID)
		return b, nil
	}

	r["backup.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			BackupID string `json:"backupId"`
		}
		json.Unmarshal(input, &in)
		// Remove cron job if backup service is available
		if h.BackupSvc != nil {
			h.BackupSvc.RemoveBackup(in.BackupID)
		}
		h.DB.Delete(&schema.Backup{}, "\"backupId\" = ?", in.BackupID)
		return true, nil
	}

	r["backup.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["backupId"].(string)

		// 只允许更新 apiUpdateBackup 定义的字段（与 TS 版 Zod schema 对齐）
		// 不更新 userId、backupType 等字段，防止覆盖外键导致记录"消失"
		allowedKeys := map[string]bool{
			"schedule": true, "enabled": true, "prefix": true,
			"destinationId": true, "database": true, "keepLatestCount": true,
			"serviceName": true, "metadata": true, "databaseType": true,
		}
		updates := make(map[string]interface{})
		for k, v := range in {
			if allowedKeys[k] {
				updates[k] = v
			}
		}
		// metadata 特殊处理：空对象 {} 存为 null，非空存为 JSON 字符串
		if m, ok := updates["metadata"]; ok {
			switch mv := m.(type) {
			case map[string]interface{}:
				if len(mv) == 0 {
					updates["metadata"] = nil
				} else {
					bs, _ := json.Marshal(mv)
					s := string(bs)
					updates["metadata"] = s
				}
			case nil:
				// metadata: null → 保持 null
			}
		}
		if err := h.DB.Model(&schema.Backup{}).Where("\"backupId\" = ?", id).Updates(updates).Error; err != nil {
			return nil, &trpcErr{"Failed to update backup: " + err.Error(), "BAD_REQUEST", 400}
		}

		// 重新调度 cron job（与 TS 版一致：enabled → remove + re-schedule，disabled → remove）
		if h.BackupSvc != nil {
			h.BackupSvc.RemoveBackup(id)
			var updated schema.Backup
			if err := h.DB.Preload("Destination").Preload("Compose").Preload("Postgres").Preload("MySQL").Preload("MariaDB").Preload("Mongo").
				First(&updated, "\"backupId\" = ?", id).Error; err == nil {
				if updated.Enabled != nil && *updated.Enabled {
					h.BackupSvc.ScheduleBackup(updated)
				}
			}
		}
		return true, nil
	}

	// listBackupFiles: 输入与 TS 版一致 {destinationId, search, serverId}
	// 使用 rclone lsjson 列出 S3 文件，返回 RcloneFile[] 数组
	r["backup.listBackupFiles"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			DestinationID string `json:"destinationId"`
			Search        string `json:"search"`
			ServerID      string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		var dest schema.Destination
		if err := h.DB.First(&dest, "\"destinationId\" = ?", in.DestinationID).Error; err != nil {
			return []rcloneFile{}, nil
		}

		rcloneFlags := getRcloneFlagsForDest(&dest)
		bucketPath := fmt.Sprintf(":s3:%s", dest.Bucket)

		// 解析搜索路径：支持目录导航（如 "appName/prefix/"）
		var baseDir, searchTerm string
		lastSlash := strings.LastIndex(in.Search, "/")
		if lastSlash != -1 {
			baseDir = normalizeListPath(in.Search[:lastSlash+1])
			searchTerm = in.Search[lastSlash+1:]
		} else {
			searchTerm = in.Search
		}

		searchPath := bucketPath
		if baseDir != "" {
			searchPath = fmt.Sprintf("%s/%s", bucketPath, baseDir)
		}

		listCmd := fmt.Sprintf(`rclone lsjson %s "%s" --no-mimetype --no-modtime 2>/dev/null`, rcloneFlags, searchPath)

		var stdout string
		if in.ServerID != "" {
			// 远程服务器执行
			var server schema.Server
			if err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", in.ServerID).Error; err != nil {
				return []rcloneFile{}, nil
			}
			if server.SSHKey != nil {
				conn := process.SSHConnection{
					Host:       server.IPAddress,
					Port:       server.Port,
					Username:   server.Username,
					PrivateKey: server.SSHKey.PrivateKey,
				}
				result, err := process.ExecAsyncRemote(conn, listCmd, nil)
				if err != nil {
					return []rcloneFile{}, nil
				}
				stdout = result.Stdout
			}
		} else {
			result, err := process.ExecAsync(listCmd)
			if err != nil {
				return []rcloneFile{}, nil
			}
			if result != nil {
				stdout = result.Stdout
			}
		}

		var files []rcloneFile
		if err := json.Unmarshal([]byte(stdout), &files); err != nil {
			return []rcloneFile{}, nil
		}

		// 如果有 baseDir，给每个文件路径加上前缀
		if baseDir != "" {
			for i := range files {
				files[i].Path = baseDir + files[i].Path
			}
		}

		// 按搜索词过滤
		if searchTerm != "" {
			lower := strings.ToLower(searchTerm)
			var filtered []rcloneFile
			for _, f := range files {
				if strings.Contains(strings.ToLower(f.Path), lower) {
					filtered = append(filtered, f)
					if len(filtered) >= 100 {
						break
					}
				}
			}
			if filtered == nil {
				filtered = []rcloneFile{}
			}
			return filtered, nil
		}

		// 限制 100 个
		if len(files) > 100 {
			files = files[:100]
		}
		return files, nil
	}

	// Manual backup endpoints - all use the same RunBackup logic
	manualBackup := func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			BackupID string `json:"backupId"`
		}
		json.Unmarshal(input, &in)
		if h.BackupSvc != nil {
			if err := h.BackupSvc.RunBackup(in.BackupID); err != nil {
				return nil, &trpcErr{err.Error(), "BAD_REQUEST", 400}
			}
		}
		return true, nil
	}

	r["backup.manualBackupPostgres"] = manualBackup
	r["backup.manualBackupMySql"] = manualBackup
	r["backup.manualBackupMariadb"] = manualBackup
	r["backup.manualBackupMongo"] = manualBackup
	r["backup.manualBackupCompose"] = manualBackup
	r["backup.manualBackupWebServer"] = manualBackup

	// Volume Backups
	r["volumeBackups.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			VolumeBackupID string `json:"volumeBackupId"`
		}
		json.Unmarshal(input, &in)
		var vb schema.VolumeBackup
		if err := h.DB.First(&vb, "\"volumeBackupId\" = ?", in.VolumeBackupID).Error; err != nil {
			return nil, &trpcErr{"Volume backup not found", "NOT_FOUND", 404}
		}
		return vb, nil
	}

	r["volumeBackups.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var vb schema.VolumeBackup
		json.Unmarshal(input, &vb)
		if err := h.DB.Create(&vb).Error; err != nil {
			return nil, err
		}
		return vb, nil
	}

	r["volumeBackups.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			VolumeBackupID string `json:"volumeBackupId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.VolumeBackup{}, "\"volumeBackupId\" = ?", in.VolumeBackupID)
		return true, nil
	}

	r["volumeBackups.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["volumeBackupId"].(string)
		delete(in, "volumeBackupId")
		in = h.filterColumns(&schema.VolumeBackup{}, in)
		h.DB.Model(&schema.VolumeBackup{}).Where("\"volumeBackupId\" = ?", id).Updates(in)
		return true, nil
	}

	r["volumeBackups.list"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID               string `json:"id"`
			VolumeBackupType string `json:"volumeBackupType"`
		}
		json.Unmarshal(input, &in)
		colMap := map[string]string{
			"application": "applicationId",
			"postgres":    "postgresId",
			"mysql":       "mysqlId",
			"mariadb":     "mariadbId",
			"mongo":       "mongoId",
			"redis":       "redisId",
			"compose":     "composeId",
		}
		col, ok := colMap[in.VolumeBackupType]
		if !ok {
			return []schema.VolumeBackup{}, nil
		}
		var vbs []schema.VolumeBackup
		h.DB.Preload("Destination").
			Preload("Application").Preload("Postgres").Preload("MySQL").
			Preload("MariaDB").Preload("Mongo").Preload("Redis").Preload("Compose").
			Where(fmt.Sprintf("\"%s\" = ?", col), in.ID).
			Order("\"createdAt\" DESC").
			Find(&vbs)
		if vbs == nil {
			vbs = []schema.VolumeBackup{}
		}
		for i := range vbs {
			if vbs[i].Deployments == nil {
				vbs[i].Deployments = []schema.Deployment{}
			}
		}
		return vbs, nil
	}

	r["volumeBackups.runManually"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			VolumeBackupID string `json:"volumeBackupId"`
		}
		json.Unmarshal(input, &in)
		if h.BackupSvc != nil {
			if err := h.BackupSvc.RunVolumeBackup(in.VolumeBackupID); err != nil {
				return nil, &trpcErr{err.Error(), "BAD_REQUEST", 400}
			}
		}
		return true, nil
	}
}

// getRcloneFlagsForDest 构建 rclone S3 认证参数（与 backup 包的 getRcloneFlags 逻辑一致）
func getRcloneFlagsForDest(dest *schema.Destination) string {
	flags := []string{
		fmt.Sprintf(`--s3-access-key-id="%s"`, dest.AccessKey),
		fmt.Sprintf(`--s3-secret-access-key="%s"`, dest.SecretAccessKey),
		fmt.Sprintf(`--s3-region="%s"`, dest.Region),
		fmt.Sprintf(`--s3-endpoint="%s"`, dest.Endpoint),
		"--s3-no-check-bucket",
		"--s3-force-path-style",
	}
	if dest.Provider != nil && *dest.Provider != "" {
		flags = append([]string{fmt.Sprintf(`--s3-provider="%s"`, *dest.Provider)}, flags...)
	}
	return strings.Join(flags, " ")
}

// normalizeListPath 规范化 S3 列表路径，确保以 / 结尾且无前导 /
func normalizeListPath(path string) string {
	p := strings.TrimSpace(path)
	p = strings.TrimLeft(p, "/")
	if p != "" && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}
