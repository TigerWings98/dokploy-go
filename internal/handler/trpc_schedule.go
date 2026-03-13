// Input: procedureRegistry, db (Schedule 表), scheduler (cron 管理)
// Output: registerScheduleTRPC - Schedule 领域的 tRPC procedure 注册
// Role: Schedule tRPC 路由注册，将 schedule.* procedure 绑定到定时任务管理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handler) registerScheduleTRPC(r procedureRegistry) {
	r["schedule.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ScheduleID string `json:"scheduleId"`
		}
		json.Unmarshal(input, &in)
		var s schema.Schedule
		if err := h.DB.First(&s, "\"scheduleId\" = ?", in.ScheduleID).Error; err != nil {
			return nil, &trpcErr{"Schedule not found", "NOT_FOUND", 404}
		}
		return s, nil
	}

	r["schedule.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var s schema.Schedule
		json.Unmarshal(input, &s)
		if err := h.DB.Create(&s).Error; err != nil {
			return nil, err
		}
		if h.Scheduler != nil {
			h.Scheduler.AddJob(s)
		}
		return s, nil
	}

	r["schedule.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ScheduleID string `json:"scheduleId"`
		}
		json.Unmarshal(input, &in)
		if h.Scheduler != nil {
			h.Scheduler.RemoveJob(in.ScheduleID)
		}
		h.DB.Delete(&schema.Schedule{}, "\"scheduleId\" = ?", in.ScheduleID)
		return true, nil
	}

	r["schedule.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in["scheduleId"].(string)
		delete(in, "scheduleId")
		in = h.filterColumns(&schema.Schedule{}, in)

		var s schema.Schedule
		if err := h.DB.First(&s, "\"scheduleId\" = ?", id).Error; err != nil {
			return nil, &trpcErr{"Schedule not found", "NOT_FOUND", 404}
		}
		h.DB.Model(&s).Updates(in)

		if h.Scheduler != nil {
			h.Scheduler.ReloadSchedule(id)
		}
		return true, nil
	}

	r["schedule.list"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID           string `json:"id"`
			ScheduleType string `json:"scheduleType"`
		}
		json.Unmarshal(input, &in)

		var schedules []schema.Schedule
		q := h.DB.Preload("Application").Preload("Server").Preload("Compose").
			Preload("Deployments", func(db *gorm.DB) *gorm.DB {
				return db.Order("\"createdAt\" DESC")
			})

		switch in.ScheduleType {
		case "application":
			q = q.Where("\"applicationId\" = ?", in.ID)
		case "compose":
			q = q.Where("\"composeId\" = ?", in.ID)
		case "server":
			q = q.Where("\"serverId\" = ?", in.ID)
		case "dokploy-server":
			q = q.Where("\"userId\" = ?", in.ID)
		}

		if err := q.Find(&schedules).Error; err != nil {
			return nil, err
		}
		if schedules == nil {
			schedules = []schema.Schedule{}
		}
		// Ensure deployments slices are never null
		for i := range schedules {
			if schedules[i].Deployments == nil {
				schedules[i].Deployments = []schema.Deployment{}
			}
		}
		return schedules, nil
	}

	r["schedule.runManually"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ScheduleID string `json:"scheduleId"`
		}
		json.Unmarshal(input, &in)

		var s schema.Schedule
		if err := h.DB.First(&s, "\"scheduleId\" = ?", in.ScheduleID).Error; err != nil {
			return nil, &trpcErr{"Schedule not found", "NOT_FOUND", 404}
		}

		if h.Scheduler != nil {
			h.Scheduler.RunNow(s)
		}
		return true, nil
	}
}
