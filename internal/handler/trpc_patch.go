package handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/process"
	"github.com/labstack/echo/v4"
)

func (h *Handler) registerPatchTRPC(r procedureRegistry) {
	r["patch.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			PatchID string `json:"patchId"`
		}
		json.Unmarshal(input, &in)
		var patch schema.Patch
		if err := h.DB.First(&patch, "\"patchId\" = ?", in.PatchID).Error; err != nil {
			return nil, &trpcErr{"Patch not found", "NOT_FOUND", 404}
		}
		return patch, nil
	}

	r["patch.byEntityId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		json.Unmarshal(input, &in)
		var patches []schema.Patch
		if in.Type == "application" {
			h.DB.Where("\"applicationId\" = ?", in.ID).Order("\"filePath\" ASC").Find(&patches)
		} else {
			h.DB.Where("\"composeId\" = ?", in.ID).Order("\"filePath\" ASC").Find(&patches)
		}
		if patches == nil {
			patches = []schema.Patch{}
		}
		return patches, nil
	}

	r["patch.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			FilePath      string  `json:"filePath"`
			Content       string  `json:"content"`
			Type          string  `json:"type"`
			Enabled       *bool   `json:"enabled"`
			ApplicationID *string `json:"applicationId"`
			ComposeID     *string `json:"composeId"`
		}
		json.Unmarshal(input, &in)
		if in.ApplicationID == nil && in.ComposeID == nil {
			return nil, &trpcErr{"Either applicationId or composeId must be provided", "BAD_REQUEST", 400}
		}
		patchType := schema.PatchTypeUpdate
		if in.Type != "" {
			patchType = schema.PatchType(in.Type)
		}
		enabled := true
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		patch := &schema.Patch{
			FilePath:      in.FilePath,
			Content:       in.Content,
			Type:          patchType,
			Enabled:       enabled,
			ApplicationID: in.ApplicationID,
			ComposeID:     in.ComposeID,
		}
		if err := h.DB.Create(patch).Error; err != nil {
			return nil, &trpcErr{"Failed to create patch: " + err.Error(), "BAD_REQUEST", 400}
		}
		return patch, nil
	}

	r["patch.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		patchID, _ := in["patchId"].(string)
		delete(in, "patchId")
		delete(in, "applicationId")
		delete(in, "composeId")
		// Ensure content ends with newline
		if content, ok := in["content"].(string); ok && content != "" {
			if !strings.HasSuffix(content, "\n") {
				in["content"] = content + "\n"
			}
		}
		now := time.Now().UTC().Format(time.RFC3339)
		in["updatedAt"] = now
		h.DB.Model(&schema.Patch{}).Where("\"patchId\" = ?", patchID).Updates(in)
		var patch schema.Patch
		h.DB.First(&patch, "\"patchId\" = ?", patchID)
		return patch, nil
	}

	r["patch.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			PatchID string `json:"patchId"`
		}
		json.Unmarshal(input, &in)
		var patch schema.Patch
		if err := h.DB.First(&patch, "\"patchId\" = ?", in.PatchID).Error; err != nil {
			return nil, &trpcErr{"Patch not found", "NOT_FOUND", 404}
		}
		h.DB.Delete(&schema.Patch{}, "\"patchId\" = ?", in.PatchID)
		return patch, nil
	}

	r["patch.toggleEnabled"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			PatchID string `json:"patchId"`
			Enabled bool   `json:"enabled"`
		}
		json.Unmarshal(input, &in)
		h.DB.Model(&schema.Patch{}).Where("\"patchId\" = ?", in.PatchID).Update("enabled", in.Enabled)
		return true, nil
	}

	r["patch.ensureRepo"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		json.Unmarshal(input, &in)
		// Find the entity to get its appName
		var appName string
		if in.Type == "application" {
			var app schema.Application
			if err := h.DB.First(&app, "\"applicationId\" = ?", in.ID).Error; err != nil {
				return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
			}
			appName = app.AppName
		} else {
			var comp schema.Compose
			if err := h.DB.First(&comp, "\"composeId\" = ?", in.ID).Error; err != nil {
				return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
			}
			appName = comp.AppName
		}
		if h.Config == nil {
			return true, nil
		}
		var basePath string
		if in.Type == "application" {
			basePath = h.Config.Paths.ApplicationsPath
		} else {
			basePath = h.Config.Paths.ComposePath
		}
		codePath := filepath.Join(basePath, appName, "code")
		if _, err := os.Stat(codePath); os.IsNotExist(err) {
			return nil, &trpcErr{"Source code directory not found. Deploy the service first.", "BAD_REQUEST", 400}
		}
		return true, nil
	}

	r["patch.readRepoDirectories"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			RepoPath string `json:"repoPath"`
		}
		json.Unmarshal(input, &in)
		codePath := h.getEntityCodePath(in.ID, in.Type)
		if codePath == "" {
			return []interface{}{}, nil
		}
		fullPath := filepath.Join(codePath, in.RepoPath)

		// Check if we need to read from remote server
		serverID := h.getEntityServerID(in.ID, in.Type)
		if serverID != nil {
			return h.readRemoteDirectories(*serverID, fullPath)
		}

		entries, err := os.ReadDir(fullPath)
		if err != nil {
			return []interface{}{}, nil
		}
		var result []map[string]interface{}
		for _, entry := range entries {
			result = append(result, map[string]interface{}{
				"name":  entry.Name(),
				"isDir": entry.IsDir(),
			})
		}
		if result == nil {
			result = []map[string]interface{}{}
		}
		return result, nil
	}

	r["patch.readRepoFile"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			FilePath string `json:"filePath"`
		}
		json.Unmarshal(input, &in)

		// Check for existing patch first
		var existing schema.Patch
		col := "\"applicationId\""
		if in.Type == "compose" {
			col = "\"composeId\""
		}
		if err := h.DB.Where("\"filePath\" = ? AND "+col+" = ?", in.FilePath, in.ID).First(&existing).Error; err == nil {
			if existing.Type == schema.PatchTypeDelete {
				return "(File not found in repo - will be removed if it exists)", nil
			}
			if existing.Content != "" {
				return existing.Content, nil
			}
		}

		codePath := h.getEntityCodePath(in.ID, in.Type)
		if codePath == "" {
			return "", nil
		}
		fullPath := filepath.Join(codePath, in.FilePath)

		serverID := h.getEntityServerID(in.ID, in.Type)
		if serverID != nil {
			return h.readRemoteFile(*serverID, fullPath)
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", nil
		}
		return string(data), nil
	}

	r["patch.saveFileAsPatch"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			FilePath  string `json:"filePath"`
			Content   string `json:"content"`
			PatchType string `json:"patchType"`
		}
		json.Unmarshal(input, &in)
		if in.PatchType == "" {
			in.PatchType = "update"
		}

		col := "\"applicationId\""
		if in.Type == "compose" {
			col = "\"composeId\""
		}

		var existing schema.Patch
		if err := h.DB.Where("\"filePath\" = ? AND "+col+" = ?", in.FilePath, in.ID).First(&existing).Error; err == nil {
			now := time.Now().UTC().Format(time.RFC3339)
			h.DB.Model(&existing).Updates(map[string]interface{}{
				"content":   in.Content,
				"type":      in.PatchType,
				"updatedAt": now,
			})
			h.DB.First(&existing, "\"patchId\" = ?", existing.PatchID)
			return existing, nil
		}

		patch := &schema.Patch{
			FilePath: in.FilePath,
			Content:  in.Content,
			Type:     schema.PatchType(in.PatchType),
			Enabled:  true,
		}
		if in.Type == "application" {
			patch.ApplicationID = &in.ID
		} else {
			patch.ComposeID = &in.ID
		}
		h.DB.Create(patch)
		return patch, nil
	}

	r["patch.markFileForDeletion"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			FilePath string `json:"filePath"`
		}
		json.Unmarshal(input, &in)

		col := "\"applicationId\""
		if in.Type == "compose" {
			col = "\"composeId\""
		}

		var existing schema.Patch
		if err := h.DB.Where("\"filePath\" = ? AND "+col+" = ?", in.FilePath, in.ID).First(&existing).Error; err == nil {
			now := time.Now().UTC().Format(time.RFC3339)
			h.DB.Model(&existing).Updates(map[string]interface{}{
				"type":      "delete",
				"content":   "",
				"updatedAt": now,
			})
			h.DB.First(&existing, "\"patchId\" = ?", existing.PatchID)
			return existing, nil
		}

		patch := &schema.Patch{
			FilePath: in.FilePath,
			Content:  "",
			Type:     schema.PatchTypeDelete,
			Enabled:  true,
		}
		if in.Type == "application" {
			patch.ApplicationID = &in.ID
		} else {
			patch.ComposeID = &in.ID
		}
		h.DB.Create(patch)
		return patch, nil
	}

	r["patch.cleanPatchRepos"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}
}

func (h *Handler) getEntityCodePath(id, entityType string) string {
	if h.Config == nil {
		return ""
	}
	var appName string
	if entityType == "application" {
		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", id).Error; err != nil {
			return ""
		}
		appName = app.AppName
		return filepath.Join(h.Config.Paths.ApplicationsPath, appName, "code")
	}
	var comp schema.Compose
	if err := h.DB.First(&comp, "\"composeId\" = ?", id).Error; err != nil {
		return ""
	}
	appName = comp.AppName
	return filepath.Join(h.Config.Paths.ComposePath, appName, "code")
}

func (h *Handler) getEntityServerID(id, entityType string) *string {
	if entityType == "application" {
		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", id).Error; err != nil {
			return nil
		}
		return app.ServerID
	}
	var comp schema.Compose
	if err := h.DB.First(&comp, "\"composeId\" = ?", id).Error; err != nil {
		return nil
	}
	return comp.ServerID
}

func (h *Handler) readRemoteDirectories(serverID, path string) (interface{}, error) {
	var srv schema.Server
	if err := h.DB.Preload("SSHKey").First(&srv, "\"serverId\" = ?", serverID).Error; err != nil {
		return []interface{}{}, nil
	}
	if srv.SSHKey == nil {
		return []interface{}{}, nil
	}
	conn := process.SSHConnection{
		Host:       srv.IPAddress,
		Port:       srv.Port,
		Username:   srv.Username,
		PrivateKey: srv.SSHKey.PrivateKey,
	}
	result, err := process.ExecAsyncRemote(conn, "ls -1pa "+path+" 2>/dev/null", nil)
	if err != nil {
		return []interface{}{}, nil
	}
	var dirs []map[string]interface{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "./" || line == "../" {
			continue
		}
		isDir := strings.HasSuffix(line, "/")
		name := strings.TrimSuffix(line, "/")
		dirs = append(dirs, map[string]interface{}{
			"name":  name,
			"isDir": isDir,
		})
	}
	if dirs == nil {
		return []map[string]interface{}{}, nil
	}
	return dirs, nil
}

func (h *Handler) readRemoteFile(serverID, path string) (string, error) {
	var srv schema.Server
	if err := h.DB.Preload("SSHKey").First(&srv, "\"serverId\" = ?", serverID).Error; err != nil {
		return "", nil
	}
	if srv.SSHKey == nil {
		return "", nil
	}
	conn := process.SSHConnection{
		Host:       srv.IPAddress,
		Port:       srv.Port,
		Username:   srv.Username,
		PrivateKey: srv.SSHKey.PrivateKey,
	}
	result, err := process.ExecAsyncRemote(conn, "cat "+path+" 2>/dev/null", nil)
	if err != nil {
		return "", nil
	}
	return result.Stdout, nil
}
