package handler

import (
	"encoding/json"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
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
