package handler

import (
	"encoding/json"

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
		return b, nil
	}

	r["backup.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			BackupID string `json:"backupId"`
		}
		json.Unmarshal(input, &in)
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
		return []interface{}{}, nil
	}

	// Manual backup endpoints
	manualBackup := func(dbType string) ProcedureFunc {
		return func(c echo.Context, input json.RawMessage) (interface{}, error) {
			// TODO: Trigger backup via backup service when implemented
			return true, nil
		}
	}

	r["backup.manualBackupPostgres"] = manualBackup("postgres")
	r["backup.manualBackupMySql"] = manualBackup("mysql")
	r["backup.manualBackupMariadb"] = manualBackup("mariadb")
	r["backup.manualBackupMongo"] = manualBackup("mongo")
	r["backup.manualBackupCompose"] = manualBackup("compose")
	r["backup.manualBackupWebServer"] = manualBackup("webserver")

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

	r["volumeBackups.runManually"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// TODO: Implement volume backup execution
		return true, nil
	}
}
