package handler

import (
	"encoding/json"
	"fmt"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerBackupTRPC(r procedureRegistry) {
	r["backup.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			BackupID string `json:"backupId"`
		}
		json.Unmarshal(input, &in)
		var b schema.Backup
		if err := h.DB.Preload("Destination").First(&b, "\"backupId\" = ?", in.BackupID).Error; err != nil {
			return nil, &trpcErr{"Backup not found", "NOT_FOUND", 404}
		}
		return b, nil
	}

	r["backup.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var b schema.Backup
		json.Unmarshal(input, &b)
		if err := h.DB.Create(&b).Error; err != nil {
			return nil, err
		}
		// Schedule backup cron job if enabled
		if h.BackupSvc != nil && b.Enabled != nil && *b.Enabled {
			h.BackupSvc.ScheduleBackup(b)
		}
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
		delete(in, "backupId")
		h.DB.Model(&schema.Backup{}).Where("\"backupId\" = ?", id).Updates(in)
		return true, nil
	}

	r["backup.listBackupFiles"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			BackupID string `json:"backupId"`
		}
		json.Unmarshal(input, &in)
		if h.BackupSvc != nil {
			files, err := h.BackupSvc.ListBackupFiles(in.BackupID)
			if err != nil {
				return []string{}, nil
			}
			return files, nil
		}
		return []string{}, nil
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
