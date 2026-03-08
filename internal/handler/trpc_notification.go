// Input: procedureRegistry, db (Notification 表), notify
// Output: registerNotificationTRPC - Notification 领域的 tRPC procedure 注册
// Role: Notification tRPC 路由注册，将 notification.* procedure 绑定到通知配置管理操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/notify"
	"github.com/labstack/echo/v4"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

func (h *Handler) registerNotificationTRPC(r procedureRegistry) {
	// Generic create notification - handles all types
	createNotification := func(notifType string) ProcedureFunc {
		return func(c echo.Context, input json.RawMessage) (interface{}, error) {
			member, err := h.getDefaultMember(c)
			if err != nil {
				return nil, err
			}

			var in map[string]interface{}
			json.Unmarshal(input, &in)

			// Create the sub-table record first
			subID, _ := gonanoid.New()
			subTable := notifType
			subIDField := notifType + "Id"

			subRecord := map[string]interface{}{
				subIDField: subID,
			}
			for k, v := range in {
				if k != "name" && k != "appDeploy" && k != "appBuildError" &&
					k != "databaseBackup" && k != "dokployRestart" && k != "dockerCleanup" &&
					k != "serverThreshold" && k != "volumeBackup" && k != "notificationType" {
					subRecord[k] = v
				}
			}

			if err := h.DB.Table(subTable).Create(subRecord).Error; err != nil {
				return nil, &trpcErr{"Failed to create " + notifType + ": " + err.Error(), "BAD_REQUEST", 400}
			}

			notif := map[string]interface{}{
				"name":             in["name"],
				"notificationType": notifType,
				"organizationId":   member.OrganizationID,
				subIDField:         subID,
			}
			for _, flag := range []string{"appDeploy", "appBuildError", "databaseBackup", "dokployRestart", "dockerCleanup", "serverThreshold", "volumeBackup"} {
				if v, ok := in[flag]; ok {
					notif[flag] = v
				}
			}

			notifID, _ := gonanoid.New()
			notif["notificationId"] = notifID
			notif["createdAt"] = time.Now().UTC().Format(time.RFC3339)

			if err := h.DB.Table("notification").Create(notif).Error; err != nil {
				h.DB.Table(subTable).Where(fmt.Sprintf("\"%s\" = ?", subIDField), subID).Delete(nil)
				return nil, err
			}

			var result map[string]interface{}
			h.DB.Table("notification").Where("\"notificationId\" = ?", notifID).First(&result)
			return result, nil
		}
	}

	updateNotification := func(notifType string) ProcedureFunc {
		return func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			notifID, _ := in["notificationId"].(string)
			delete(in, "notificationId")

			subIDField := notifType + "Id"
			subTable := notifType

			var notif map[string]interface{}
			h.DB.Table("notification").Where("\"notificationId\" = ?", notifID).First(&notif)

			mainFields := map[string]interface{}{}
			subFields := map[string]interface{}{}
			mainFieldNames := map[string]bool{
				"name": true, "appDeploy": true, "appBuildError": true,
				"databaseBackup": true, "dokployRestart": true, "dockerCleanup": true,
				"serverThreshold": true, "volumeBackup": true,
			}
			for k, v := range in {
				if mainFieldNames[k] {
					mainFields[k] = v
				} else {
					subFields[k] = v
				}
			}

			if len(mainFields) > 0 {
				h.DB.Table("notification").Where("\"notificationId\" = ?", notifID).Updates(mainFields)
			}

			if len(subFields) > 0 {
				if subID, ok := notif[subIDField]; ok && subID != nil {
					h.DB.Table(subTable).Where(fmt.Sprintf("\"%s\" = ?", subIDField), subID).Updates(subFields)
				}
			}

			return true, nil
		}
	}

	testNotification := func(notifType string) ProcedureFunc {
		return func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in struct {
				NotificationID string `json:"notificationId"`
			}
			json.Unmarshal(input, &in)
			if in.NotificationID == "" {
				return nil, &trpcErr{"notificationId is required", "BAD_REQUEST", 400}
			}
			var notif schema.Notification
			if err := h.DB.First(&notif, "\"notificationId\" = ?", in.NotificationID).Error; err != nil {
				return nil, &trpcErr{"Notification not found", "NOT_FOUND", 404}
			}
			if h.Notifier != nil {
				if err := h.Notifier.SendTest(&notif); err != nil {
					return nil, &trpcErr{"Test notification failed: " + err.Error(), "BAD_REQUEST", 400}
				}
			}
			return true, nil
		}
	}

	types := []string{"slack", "telegram", "discord", "email", "resend", "gotify", "ntfy", "custom", "lark", "teams", "pushover"}
	for _, t := range types {
		capitalFirst := strings.ToUpper(t[:1]) + t[1:]
		r["notification.create"+capitalFirst] = createNotification(t)
		r["notification.update"+capitalFirst] = updateNotification(t)
		r["notification.test"+capitalFirst+"Connection"] = testNotification(t)
	}

	r["notification.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			NotificationID string `json:"notificationId"`
		}
		json.Unmarshal(input, &in)
		h.DB.Table("notification").Where("\"notificationId\" = ?", in.NotificationID).Delete(map[string]interface{}{})
		return true, nil
	}

	r["notification.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			NotificationID string `json:"notificationId"`
		}
		json.Unmarshal(input, &in)
		var notif map[string]interface{}
		if err := h.DB.Table("notification").Where("\"notificationId\" = ?", in.NotificationID).
			First(&notif).Error; err != nil {
			return nil, &trpcErr{"Notification not found", "NOT_FOUND", 404}
		}
		return notif, nil
	}

	r["notification.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var notifs []map[string]interface{}
		h.DB.Table("notification").
			Where("\"organizationId\" = ?", member.OrganizationID).
			Find(&notifs)
		if notifs == nil {
			notifs = []map[string]interface{}{}
		}
		return notifs, nil
	}

	// receiveNotification - receives monitoring alerts and dispatches notifications
	r["notification.receiveNotification"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ServerType string  `json:"ServerType"`
			Type       string  `json:"Type"`
			Value      float64 `json:"Value"`
			Threshold  float64 `json:"Threshold"`
			Message    string  `json:"Message"`
			Timestamp  string  `json:"Timestamp"`
			Token      string  `json:"Token"`
		}
		json.Unmarshal(input, &in)

		if h.Notifier != nil {
			msg := fmt.Sprintf("[%s] %s alert: %.1f%% (threshold: %.1f%%)\n%s",
				in.ServerType, in.Type, in.Value, in.Threshold, in.Message)

			// Get all organization IDs that have serverThreshold notifications
			var orgIDs []string
			h.DB.Table("notification").
				Where("\"serverThreshold\" = ?", true).
				Distinct("\"organizationId\"").
				Pluck("\"organizationId\"", &orgIDs)

			for _, orgID := range orgIDs {
				h.Notifier.Send(orgID, notify.NotificationPayload{
					Event:   notify.EventDokployRestart,
					Title:   "Server Alert",
					Message: msg,
				})
			}
		}

		return true, nil
	}

	r["notification.getEmailProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := h.getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var notifs []map[string]interface{}
		h.DB.Table("notification").
			Where("\"organizationId\" = ? AND \"notificationType\" IN ?", member.OrganizationID, []string{"email", "resend"}).
			Find(&notifs)
		return notifs, nil
	}
}
