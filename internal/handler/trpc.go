package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
	mw "github.com/dokploy/dokploy/internal/middleware"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// TRPCRequest represents a superjson-wrapped tRPC input.
type TRPCRequest struct {
	JSON json.RawMessage `json:"json"`
	Meta json.RawMessage `json:"meta,omitempty"`
}

// TRPCResponse wraps a result in the tRPC response format.
type TRPCResponse struct {
	Result TRPCResult `json:"result"`
}

type TRPCResult struct {
	Data TRPCData `json:"data"`
}

type TRPCData struct {
	JSON interface{} `json:"json"`
}

// TRPCError represents a tRPC error response.
type TRPCErrorResponse struct {
	Error TRPCErrorData `json:"error"`
}

type TRPCErrorData struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
	Data    *TRPCErrorInfo `json:"data,omitempty"`
}

type TRPCErrorInfo struct {
	Code       string `json:"code"`
	HTTPStatus int    `json:"httpStatus"`
}

// ProcedureFunc is a function that handles a tRPC procedure.
// It receives the parsed JSON input and returns the result.
type ProcedureFunc func(c echo.Context, input json.RawMessage) (interface{}, error)

// procedureRegistry maps "router.procedure" to handler functions.
type procedureRegistry map[string]ProcedureFunc

// HandleTRPC is the main tRPC compatibility handler.
// It handles both batched and non-batched requests.
func (h *Handler) HandleTRPC(c echo.Context) error {
	procedures := c.Param("procedures")
	isBatch := c.QueryParam("batch") == "1"

	if procedures == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "No procedure specified")
	}

	procNames := strings.Split(procedures, ",")

	if isBatch {
		return h.handleBatchTRPC(c, procNames)
	}

	// Single procedure
	if len(procNames) != 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "Multiple procedures require batch=1")
	}

	input, err := h.extractInput(c, "")
	if err != nil {
		return h.trpcError(c, err.Error(), "BAD_REQUEST", http.StatusBadRequest)
	}

	result, err := h.callProcedure(c, procNames[0], input)
	if err != nil {
		return h.handleProcedureError(c, err)
	}

	return c.JSON(http.StatusOK, TRPCResponse{
		Result: TRPCResult{Data: TRPCData{JSON: result}},
	})
}

func (h *Handler) handleBatchTRPC(c echo.Context, procNames []string) error {
	results := make([]interface{}, len(procNames))

	for i, proc := range procNames {
		idx := fmt.Sprintf("%d", i)
		input, err := h.extractInput(c, idx)
		if err != nil {
			results[i] = TRPCErrorResponse{
				Error: TRPCErrorData{
					Message: err.Error(),
					Code:    -32600,
					Data:    &TRPCErrorInfo{Code: "BAD_REQUEST", HTTPStatus: 400},
				},
			}
			continue
		}

		result, err := h.callProcedure(c, proc, input)
		if err != nil {
			results[i] = h.buildErrorResult(err)
		} else {
			results[i] = TRPCResponse{
				Result: TRPCResult{Data: TRPCData{JSON: result}},
			}
		}
	}

	return c.JSON(http.StatusOK, results)
}

// extractInput extracts the JSON input from a tRPC request.
// For GET: from query param "input" (URL-encoded JSON)
// For POST: from request body
// batchIdx is empty for non-batch, or "0","1",... for batch.
func (h *Handler) extractInput(c echo.Context, batchIdx string) (json.RawMessage, error) {
	if c.Request().Method == http.MethodGet {
		inputStr := c.QueryParam("input")
		if inputStr == "" {
			return nil, nil
		}

		decoded, err := url.QueryUnescape(inputStr)
		if err != nil {
			decoded = inputStr
		}

		if batchIdx != "" {
			// Batch GET: input is {"0":{"json":{...}},"1":{"json":{...}}}
			var batchInput map[string]TRPCRequest
			if err := json.Unmarshal([]byte(decoded), &batchInput); err != nil {
				return nil, fmt.Errorf("invalid batch input: %w", err)
			}
			if req, ok := batchInput[batchIdx]; ok {
				return req.JSON, nil
			}
			return nil, nil
		}

		// Non-batch GET: input is {"json":{...}}
		var req TRPCRequest
		if err := json.Unmarshal([]byte(decoded), &req); err != nil {
			// Try raw JSON (without superjson wrapper)
			return []byte(decoded), nil
		}
		if req.JSON != nil {
			return req.JSON, nil
		}
		return []byte(decoded), nil
	}

	// POST
	body := make(map[string]json.RawMessage)
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
		return nil, nil
	}

	if batchIdx != "" {
		// Batch POST: body is {"0":{"json":{...}},"1":{"json":{...}}}
		raw, ok := body[batchIdx]
		if !ok {
			return nil, nil
		}
		var req TRPCRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return raw, nil
		}
		if req.JSON != nil {
			return req.JSON, nil
		}
		return raw, nil
	}

	// Non-batch POST: body is {"json":{...}}
	if jsonData, ok := body["json"]; ok {
		return jsonData, nil
	}
	// Fallback: treat whole body as input
	all, _ := json.Marshal(body)
	return all, nil
}

func (h *Handler) callProcedure(c echo.Context, name string, input json.RawMessage) (interface{}, error) {
	registry := h.buildRegistry()

	fn, ok := registry[name]
	if !ok {
		return nil, &trpcErr{message: fmt.Sprintf("No procedure '%s' found", name), code: "NOT_FOUND", status: 404}
	}

	return fn(c, input)
}

type trpcErr struct {
	message string
	code    string
	status  int
}

func (e *trpcErr) Error() string { return e.message }

func (h *Handler) trpcError(c echo.Context, message, code string, status int) error {
	return c.JSON(status, TRPCErrorResponse{
		Error: TRPCErrorData{
			Message: message,
			Code:    -32600,
			Data:    &TRPCErrorInfo{Code: code, HTTPStatus: status},
		},
	})
}

func (h *Handler) handleProcedureError(c echo.Context, err error) error {
	if te, ok := err.(*trpcErr); ok {
		return h.trpcError(c, te.message, te.code, te.status)
	}
	return h.trpcError(c, err.Error(), "INTERNAL_SERVER_ERROR", 500)
}

func (h *Handler) buildErrorResult(err error) interface{} {
	code := "INTERNAL_SERVER_ERROR"
	status := 500
	if te, ok := err.(*trpcErr); ok {
		code = te.code
		status = te.status
	}
	return TRPCErrorResponse{
		Error: TRPCErrorData{
			Message: err.Error(),
			Code:    -32600,
			Data:    &TRPCErrorInfo{Code: code, HTTPStatus: status},
		},
	}
}

// --- Registry builder ---

func (h *Handler) buildRegistry() procedureRegistry {
	r := make(procedureRegistry)

	// Helper to get default org member
	getDefaultMember := func(c echo.Context) (*schema.Member, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Authentication required", "UNAUTHORIZED", 401}
		}
		var member schema.Member
		if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
			return nil, &trpcErr{"No default organization found", "BAD_REQUEST", 400}
		}
		return &member, nil
	}

	// ===================== PROJECT =====================
	r["project.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var projects []schema.Project
		if err := h.DB.
			Preload("Environments").
			Where("\"organizationId\" = ?", member.OrganizationID).
			Order("\"createdAt\" DESC").
			Find(&projects).Error; err != nil {
			return nil, err
		}
		return projects, nil
	}

	r["project.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ProjectID string `json:"projectId"` }
		json.Unmarshal(input, &in)
		var project schema.Project
		err := h.DB.
			Preload("Environments").
			First(&project, "\"projectId\" = ?", in.ProjectID).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, &trpcErr{"Project not found", "NOT_FOUND", 404}
			}
			return nil, err
		}
		return project, nil
	}

	r["project.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			Name        string  `json:"name"`
			Description *string `json:"description"`
		}
		json.Unmarshal(input, &in)

		project := &schema.Project{
			Name:           in.Name,
			Description:    in.Description,
			OrganizationID: member.OrganizationID,
		}
		if err := h.DB.Create(project).Error; err != nil {
			return nil, err
		}
		return project, nil
	}

	r["project.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ProjectID string `json:"projectId"` }
		json.Unmarshal(input, &in)
		result := h.DB.Delete(&schema.Project{}, "\"projectId\" = ?", in.ProjectID)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, &trpcErr{"Project not found", "NOT_FOUND", 404}
		}
		return true, nil
	}

	r["project.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		projectID, _ := in["projectId"].(string)
		delete(in, "projectId")

		var project schema.Project
		if err := h.DB.First(&project, "\"projectId\" = ?", projectID).Error; err != nil {
			return nil, &trpcErr{"Project not found", "NOT_FOUND", 404}
		}
		if err := h.DB.Model(&project).Updates(in).Error; err != nil {
			return nil, err
		}
		return project, nil
	}

	// ===================== APPLICATION =====================
	h.registerApplicationTRPC(r, getDefaultMember)

	// ===================== COMPOSE =====================
	h.registerComposeTRPC(r)

	// ===================== DATABASES =====================
	h.registerDatabaseTRPC(r, "postgres", "Postgres", "postgresId")
	h.registerDatabaseTRPC(r, "mysql", "MySql", "mysqlId")
	h.registerDatabaseTRPC(r, "mariadb", "Mariadb", "mariadbId")
	h.registerDatabaseTRPC(r, "mongo", "Mongo", "mongoId")
	h.registerDatabaseTRPC(r, "redis", "Redis", "redisId")

	// ===================== DEPLOYMENT =====================
	h.registerDeploymentTRPC(r)

	// ===================== DOMAIN =====================
	h.registerDomainTRPC(r)

	// ===================== SERVER =====================
	h.registerServerTRPC(r, getDefaultMember)

	// ===================== ORGANIZATION =====================
	h.registerOrganizationTRPC(r, getDefaultMember)

	// ===================== SETTINGS =====================
	h.registerSettingsTRPC(r)

	// ===================== USER =====================
	h.registerUserTRPC(r)

	// ===================== NOTIFICATION =====================
	h.registerNotificationTRPC(r)

	// ===================== GIT PROVIDERS =====================
	h.registerGitProviderTRPC(r, getDefaultMember)

	// ===================== SIMPLE CRUD ROUTERS =====================
	h.registerSimpleCRUDTRPC(r, getDefaultMember)

	// ===================== SSO =====================
	r["sso.showSignInWithSSO"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return false, nil
	}

	// ===================== ADMIN =====================
	r["admin.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		settings, err := h.getOrCreateSettings()
		if err != nil {
			return nil, err
		}
		return settings, nil
	}

	// ===================== AUTH-RELATED QUERIES =====================
	r["auth.isAdminPresent"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return h.DB.IsAdminPresent(), nil
	}

	return r
}

func (h *Handler) registerApplicationTRPC(r procedureRegistry, getDefaultMember func(echo.Context) (*schema.Member, error)) {
	r["application.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		_ = member
		var in struct {
			Name          string  `json:"name"`
			Description   *string `json:"description"`
			EnvironmentID string  `json:"environmentId"`
			ServerID      *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)

		app := &schema.Application{
			Name:          in.Name,
			Description:   in.Description,
			EnvironmentID: in.EnvironmentID,
			ServerID:      in.ServerID,
		}
		if err := h.DB.Create(app).Error; err != nil {
			return nil, err
		}
		return app, nil
	}

	r["application.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)

		var app schema.Application
		err := h.DB.
			Preload("Deployments", func(db *gorm.DB) *gorm.DB {
				return db.Order("\"createdAt\" DESC").Limit(10)
			}).
			Preload("Domains").
			Preload("Mounts").
			Preload("Redirects").
			Preload("Security").
			Preload("Ports").
			Preload("Registry").
			Preload("Server").
			First(&app, "\"applicationId\" = ?", in.ApplicationID).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
			}
			return nil, err
		}
		return app, nil
	}

	r["application.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		result := h.DB.Delete(&schema.Application{}, "\"applicationId\" = ?", in.ApplicationID)
		if result.Error != nil {
			return nil, result.Error
		}
		return true, nil
	}

	r["application.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		appID, _ := in["applicationId"].(string)
		delete(in, "applicationId")

		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", appID).Error; err != nil {
			return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
		}
		if err := h.DB.Model(&app).Updates(in).Error; err != nil {
			return nil, err
		}
		return app, nil
	}

	r["application.deploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ApplicationID string  `json:"applicationId"`
			Title         *string `json:"title"`
			Description   *string `json:"description"`
		}
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			info, err := h.Queue.EnqueueDeployApplication(in.ApplicationID, in.Title, in.Description)
			if err != nil {
				return nil, err
			}
			return map[string]string{"message": "Deployment queued", "taskId": info.ID}, nil
		}
		return true, nil
	}

	r["application.redeploy"] = r["application.deploy"]

	r["application.stop"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			_, err := h.Queue.EnqueueStopApplication(in.ApplicationID)
			if err != nil {
				return nil, err
			}
		}
		return true, nil
	}

	r["application.start"] = r["application.deploy"]

	r["application.reload"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		// Reload just re-deploys
		if h.Queue != nil {
			title := "Reload"
			_, err := h.Queue.EnqueueDeployApplication(in.ApplicationID, &title, nil)
			if err != nil {
				return nil, err
			}
		}
		return true, nil
	}

	r["application.refreshToken"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", in.ApplicationID).Error; err != nil {
			return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
		}
		newToken := schema.GenerateAppName("refresh")
		h.DB.Model(&app).Update("refreshToken", newToken)
		return map[string]string{"token": newToken}, nil
	}

	// saveEnvironment, saveBuildType, saveXxxProvider - generic update
	for _, proc := range []string{
		"saveEnvironment", "saveBuildType",
		"saveGithubProvider", "saveGitlabProvider", "saveBitbucketProvider",
		"saveGiteaProvider", "saveDockerProvider", "saveGitProvider",
		"disconnectGitProvider", "markRunning",
		"updateTraefikConfig", "cancelDeployment", "cleanQueues",
	} {
		procName := proc
		r["application."+procName] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
			var in map[string]interface{}
			json.Unmarshal(input, &in)
			appID, _ := in["applicationId"].(string)
			delete(in, "applicationId")

			var app schema.Application
			if err := h.DB.First(&app, "\"applicationId\" = ?", appID).Error; err != nil {
				return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
			}
			if len(in) > 0 {
				h.DB.Model(&app).Updates(in)
			}
			return app, nil
		}
	}

	r["application.readTraefikConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		var app schema.Application
		if err := h.DB.First(&app, "\"applicationId\" = ?", in.ApplicationID).Error; err != nil {
			return nil, &trpcErr{"Application not found", "NOT_FOUND", 404}
		}
		if h.Traefik != nil {
			config, _ := h.Traefik.ReadServiceConfig(app.AppName)
			return config, nil
		}
		return "", nil
	}
}

func (h *Handler) registerComposeTRPC(r procedureRegistry) {
	r["compose.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		var compose schema.Compose
		err := h.DB.
			Preload("Deployments", func(db *gorm.DB) *gorm.DB {
				return db.Order("\"createdAt\" DESC").Limit(10)
			}).
			Preload("Domains").
			Preload("Mounts").
			Preload("Server").
			First(&compose, "\"composeId\" = ?", in.ComposeID).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
			}
			return nil, err
		}
		return compose, nil
	}

	r["compose.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Name          string  `json:"name"`
			Description   *string `json:"description"`
			EnvironmentID string  `json:"environmentId"`
			ServerID      *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		compose := &schema.Compose{
			Name:          in.Name,
			Description:   in.Description,
			EnvironmentID: in.EnvironmentID,
			ServerID:      in.ServerID,
		}
		if err := h.DB.Create(compose).Error; err != nil {
			return nil, err
		}
		return compose, nil
	}

	r["compose.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		composeID, _ := in["composeId"].(string)
		delete(in, "composeId")
		var compose schema.Compose
		if err := h.DB.First(&compose, "\"composeId\" = ?", composeID).Error; err != nil {
			return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
		}
		if err := h.DB.Model(&compose).Updates(in).Error; err != nil {
			return nil, err
		}
		return compose, nil
	}

	r["compose.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		result := h.DB.Delete(&schema.Compose{}, "\"composeId\" = ?", in.ComposeID)
		if result.Error != nil {
			return nil, result.Error
		}
		return true, nil
	}

	r["compose.deploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ComposeID string  `json:"composeId"`
			Title     *string `json:"title"`
		}
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			info, err := h.Queue.EnqueueDeployCompose(in.ComposeID, in.Title)
			if err != nil {
				return nil, err
			}
			return map[string]string{"message": "Deployment queued", "taskId": info.ID}, nil
		}
		return true, nil
	}

	r["compose.redeploy"] = r["compose.deploy"]
	r["compose.start"] = r["compose.deploy"]

	r["compose.stop"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		if h.Queue != nil {
			_, err := h.Queue.EnqueueStopCompose(in.ComposeID)
			if err != nil {
				return nil, err
			}
		}
		return true, nil
	}

	r["compose.refreshToken"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		var compose schema.Compose
		if err := h.DB.First(&compose, "\"composeId\" = ?", in.ComposeID).Error; err != nil {
			return nil, &trpcErr{"Compose not found", "NOT_FOUND", 404}
		}
		newToken := schema.GenerateAppName("refresh")
		h.DB.Model(&compose).Update("refreshToken", newToken)
		return map[string]string{"token": newToken}, nil
	}
}

func (h *Handler) registerDeploymentTRPC(r procedureRegistry) {
	r["deployment.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		var deployments []schema.Deployment
		h.DB.Where("\"applicationId\" = ?", in.ApplicationID).
			Order("\"createdAt\" DESC").
			Find(&deployments)
		return deployments, nil
	}

	r["deployment.allByCompose"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		var deployments []schema.Deployment
		h.DB.Where("\"composeId\" = ?", in.ComposeID).
			Order("\"createdAt\" DESC").
			Find(&deployments)
		return deployments, nil
	}

	r["deployment.allByServer"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID string `json:"serverId"` }
		json.Unmarshal(input, &in)
		var deployments []schema.Deployment
		h.DB.Where("\"serverId\" = ?", in.ServerID).
			Order("\"createdAt\" DESC").
			Find(&deployments)
		return deployments, nil
	}

	r["deployment.removeDeployment"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ DeploymentID string `json:"deploymentId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Deployment{}, "\"deploymentId\" = ?", in.DeploymentID)
		return true, nil
	}
}

func (h *Handler) registerDomainTRPC(r procedureRegistry) {
	r["domain.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var domain schema.Domain
		json.Unmarshal(input, &domain)
		if err := h.DB.Create(&domain).Error; err != nil {
			return nil, err
		}
		return domain, nil
	}

	r["domain.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ DomainID string `json:"domainId"` }
		json.Unmarshal(input, &in)
		var domain schema.Domain
		if err := h.DB.First(&domain, "\"domainId\" = ?", in.DomainID).Error; err != nil {
			return nil, &trpcErr{"Domain not found", "NOT_FOUND", 404}
		}
		return domain, nil
	}

	r["domain.byApplicationId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		var domains []schema.Domain
		h.DB.Where("\"applicationId\" = ?", in.ApplicationID).Find(&domains)
		return domains, nil
	}

	r["domain.byComposeId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ComposeID string `json:"composeId"` }
		json.Unmarshal(input, &in)
		var domains []schema.Domain
		h.DB.Where("\"composeId\" = ?", in.ComposeID).Find(&domains)
		return domains, nil
	}

	r["domain.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		domainID, _ := in["domainId"].(string)
		delete(in, "domainId")
		var domain schema.Domain
		if err := h.DB.First(&domain, "\"domainId\" = ?", domainID).Error; err != nil {
			return nil, &trpcErr{"Domain not found", "NOT_FOUND", 404}
		}
		h.DB.Model(&domain).Updates(in)
		return domain, nil
	}

	r["domain.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ DomainID string `json:"domainId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Domain{}, "\"domainId\" = ?", in.DomainID)
		return true, nil
	}
}

func (h *Handler) registerServerTRPC(r procedureRegistry, getDefaultMember func(echo.Context) (*schema.Member, error)) {
	r["server.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var servers []schema.Server
		h.DB.Preload("SSHKey").
			Where("\"organizationId\" = ?", member.OrganizationID).
			Find(&servers)
		return servers, nil
	}

	r["server.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID string `json:"serverId"` }
		json.Unmarshal(input, &in)
		var server schema.Server
		err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", in.ServerID).Error
		if err != nil {
			return nil, &trpcErr{"Server not found", "NOT_FOUND", 404}
		}
		return server, nil
	}

	r["server.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var in struct {
			Name      string  `json:"name"`
			IPAddress string  `json:"ipAddress"`
			Port      int     `json:"port"`
			Username  string  `json:"username"`
			SSHKeyID  *string `json:"sshKeyId"`
		}
		json.Unmarshal(input, &in)
		server := &schema.Server{
			Name:           in.Name,
			IPAddress:      in.IPAddress,
			Port:           in.Port,
			Username:       in.Username,
			SSHKeyID:       in.SSHKeyID,
			OrganizationID: member.OrganizationID,
		}
		if err := h.DB.Create(server).Error; err != nil {
			return nil, err
		}
		return server, nil
	}

	r["server.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerID string `json:"serverId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Server{}, "\"serverId\" = ?", in.ServerID)
		return true, nil
	}

	r["server.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		serverID, _ := in["serverId"].(string)
		delete(in, "serverId")
		var server schema.Server
		if err := h.DB.First(&server, "\"serverId\" = ?", serverID).Error; err != nil {
			return nil, &trpcErr{"Server not found", "NOT_FOUND", 404}
		}
		h.DB.Model(&server).Updates(in)
		return server, nil
	}
}

func (h *Handler) registerOrganizationTRPC(r procedureRegistry, getDefaultMember func(echo.Context) (*schema.Member, error)) {
	r["organization.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var orgs []schema.Organization
		h.DB.Where("id IN (SELECT organization_id FROM member WHERE user_id = ?)", user.ID).
			Preload("Members", "user_id = ?", user.ID).
			Find(&orgs)
		return orgs, nil
	}

	r["organization.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ OrganizationID string `json:"organizationId"` }
		json.Unmarshal(input, &in)
		var org schema.Organization
		if err := h.DB.First(&org, "id = ?", in.OrganizationID).Error; err != nil {
			return nil, &trpcErr{"Organization not found", "NOT_FOUND", 404}
		}
		return org, nil
	}

	r["organization.active"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, nil
		}
		var org schema.Organization
		if err := h.DB.First(&org, "id = ?", member.OrganizationID).Error; err != nil {
			return nil, nil
		}
		return org, nil
	}

	r["organization.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var in struct {
			Name string  `json:"name"`
			Logo *string `json:"logo"`
		}
		json.Unmarshal(input, &in)

		org := &schema.Organization{Name: in.Name, Logo: in.Logo, OwnerID: user.ID}
		tx := h.DB.Begin()
		if err := tx.Create(org).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
		member := &schema.Member{OrganizationID: org.ID, UserID: user.ID, Role: schema.MemberRoleOwner}
		if err := tx.Create(member).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
		tx.Commit()
		return org, nil
	}

	r["organization.update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			OrganizationID string  `json:"organizationId"`
			Name           string  `json:"name"`
			Logo           *string `json:"logo"`
		}
		json.Unmarshal(input, &in)
		var org schema.Organization
		if err := h.DB.First(&org, "id = ?", in.OrganizationID).Error; err != nil {
			return nil, &trpcErr{"Organization not found", "NOT_FOUND", 404}
		}
		updates := map[string]interface{}{"name": in.Name}
		if in.Logo != nil {
			updates["logo"] = *in.Logo
		}
		h.DB.Model(&org).Updates(updates)
		return org, nil
	}

	r["organization.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ OrganizationID string `json:"organizationId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Organization{}, "id = ?", in.OrganizationID)
		return true, nil
	}

	r["organization.setDefault"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var in struct{ OrganizationID string `json:"organizationId"` }
		json.Unmarshal(input, &in)
		tx := h.DB.Begin()
		tx.Model(&schema.Member{}).Where("user_id = ?", user.ID).Update("is_default", false)
		tx.Model(&schema.Member{}).Where("user_id = ? AND organization_id = ?", user.ID, in.OrganizationID).Update("is_default", true)
		tx.Commit()
		return map[string]bool{"success": true}, nil
	}

	r["organization.allInvitations"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var invitations []schema.Invitation
		h.DB.Where("organization_id = ?", member.OrganizationID).
			Order("status DESC, expires_at DESC").
			Find(&invitations)
		return invitations, nil
	}

	r["organization.removeInvitation"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ InvitationID string `json:"invitationId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Invitation{}, "id = ?", in.InvitationID)
		return true, nil
	}

	r["organization.updateMemberRole"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			MemberID string `json:"memberId"`
			Role     string `json:"role"`
		}
		json.Unmarshal(input, &in)
		h.DB.Model(&schema.Member{}).Where("id = ?", in.MemberID).Update("role", in.Role)
		return true, nil
	}
}

func (h *Handler) registerSettingsTRPC(r procedureRegistry) {
	r["settings.getWebServerSettings"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		settings, err := h.getOrCreateSettings()
		if err != nil {
			return nil, err
		}
		return settings, nil
	}

	r["settings.isCloud"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return false, nil
	}

	r["settings.getDokployVersion"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return "canary", nil
	}

	r["settings.health"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return map[string]string{"status": "ok"}, nil
	}

	r["settings.getIp"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		settings, _ := h.getOrCreateSettings()
		if settings != nil && settings.ServerIP != nil {
			return *settings.ServerIP, nil
		}
		return "", nil
	}

	r["settings.assignDomainServer"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			Host             string `json:"host"`
			CertificateType  string `json:"certificateType"`
			LetsEncryptEmail *string `json:"letsEncryptEmail"`
			HTTPS            *bool  `json:"https"`
		}
		json.Unmarshal(input, &in)
		settings, err := h.getOrCreateSettings()
		if err != nil {
			return nil, err
		}
		updates := map[string]interface{}{
			"host": in.Host,
			"certificateType": in.CertificateType,
			"letsEncryptEmail": in.LetsEncryptEmail,
		}
		if in.HTTPS != nil {
			updates["https"] = *in.HTTPS
		}
		h.DB.Model(settings).Updates(updates)
		return settings, nil
	}

	r["settings.updateServerIp"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ServerIP string `json:"serverIp"` }
		json.Unmarshal(input, &in)
		settings, _ := h.getOrCreateSettings()
		h.DB.Model(settings).Update("serverIp", in.ServerIP)
		return settings, nil
	}

	r["settings.cleanUnusedImages"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Docker != nil {
			h.Docker.CleanupImages(c.Request().Context())
		}
		return true, nil
	}

	r["settings.cleanUnusedVolumes"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Docker != nil {
			h.Docker.CleanupVolumes(c.Request().Context())
		}
		return true, nil
	}

	r["settings.cleanStoppedContainers"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Docker != nil {
			h.Docker.CleanupContainers(c.Request().Context())
		}
		return true, nil
	}

	r["settings.cleanAll"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Docker != nil {
			ctx := c.Request().Context()
			h.Docker.CleanupImages(ctx)
			h.Docker.CleanupVolumes(ctx)
			h.Docker.CleanupContainers(ctx)
		}
		return true, nil
	}

	r["settings.cleanDockerBuilder"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Docker != nil {
			h.Docker.CleanupBuildCache(c.Request().Context())
		}
		return true, nil
	}

	r["settings.readTraefikConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Traefik != nil {
			config, _ := h.Traefik.ReadMainConfig()
			return config, nil
		}
		return "", nil
	}

	r["settings.updateTraefikConfig"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ TraefikConfig string `json:"traefikConfig"` }
		json.Unmarshal(input, &in)
		if h.Traefik != nil {
			h.Traefik.WriteMainConfig(in.TraefikConfig)
		}
		return true, nil
	}

	r["settings.reloadTraefik"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["settings.reloadServer"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		return true, nil
	}

	r["settings.updateDockerCleanup"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			EnableDockerCleanup bool    `json:"enableDockerCleanup"`
			ServerID            *string `json:"serverId"`
		}
		json.Unmarshal(input, &in)
		if in.ServerID != nil {
			h.DB.Model(&schema.Server{}).Where("\"serverId\" = ?", *in.ServerID).
				Update("enableDockerCleanup", in.EnableDockerCleanup)
		} else {
			settings, _ := h.getOrCreateSettings()
			h.DB.Model(settings).Update("enableDockerCleanup", in.EnableDockerCleanup)
		}
		return true, nil
	}

	r["settings.saveSSHPrivateKey"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ SSHPrivateKey string `json:"sshPrivateKey"` }
		json.Unmarshal(input, &in)
		settings, _ := h.getOrCreateSettings()
		h.DB.Model(settings).Update("sshPrivateKey", in.SSHPrivateKey)
		return true, nil
	}

	r["settings.cleanSSHPrivateKey"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		settings, _ := h.getOrCreateSettings()
		h.DB.Model(settings).Update("sshPrivateKey", nil)
		return true, nil
	}
}

func (h *Handler) registerUserTRPC(r procedureRegistry) {
	r["user.get"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		return user, nil
	}

	r["user.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var users []schema.User
		h.DB.Find(&users)
		return users, nil
	}

	r["user.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ UserID string `json:"userId"` }
		json.Unmarshal(input, &in)
		var user schema.User
		if err := h.DB.First(&user, "id = ?", in.UserID).Error; err != nil {
			return nil, &trpcErr{"User not found", "NOT_FOUND", 404}
		}
		return user, nil
	}

	r["user.haveRootAccess"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return false, nil
		}
		return user.Role == "admin", nil
	}
}

func (h *Handler) registerNotificationTRPC(r procedureRegistry) {
	r["notification.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		user := mw.GetUser(c)
		if user == nil {
			return nil, &trpcErr{"Unauthorized", "UNAUTHORIZED", 401}
		}
		var member schema.Member
		if err := h.DB.Where("user_id = ? AND is_default = ?", user.ID, true).First(&member).Error; err != nil {
			return []schema.Notification{}, nil
		}
		var notifications []schema.Notification
		h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&notifications)
		return notifications, nil
	}

	r["notification.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ NotificationID string `json:"notificationId"` }
		json.Unmarshal(input, &in)
		var n schema.Notification
		if err := h.DB.First(&n, "\"notificationId\" = ?", in.NotificationID).Error; err != nil {
			return nil, &trpcErr{"Notification not found", "NOT_FOUND", 404}
		}
		return n, nil
	}

	r["notification.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ NotificationID string `json:"notificationId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Notification{}, "\"notificationId\" = ?", in.NotificationID)
		return true, nil
	}
}

func (h *Handler) registerGitProviderTRPC(r procedureRegistry, getDefaultMember func(echo.Context) (*schema.Member, error)) {
	r["gitProvider.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var providers []schema.GitProvider
		h.DB.Preload("Github").Preload("Gitlab").Preload("Bitbucket").Preload("Gitea").
			Where("\"organizationId\" = ?", member.OrganizationID).
			Find(&providers)
		return providers, nil
	}

	// GitHub
	r["github.githubProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var providers []schema.Github
		h.DB.Preload("GitProvider").
			Joins("JOIN git_provider ON git_provider.\"gitProviderId\" = github.\"gitProviderId\"").
			Where("git_provider.\"organizationId\" = ?", member.OrganizationID).
			Where("github.\"githubAppId\" IS NOT NULL AND github.\"githubPrivateKey\" IS NOT NULL AND github.\"githubInstallationId\" IS NOT NULL").
			Find(&providers)
		return providers, nil
	}

	r["github.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ GithubID string `json:"githubId"` }
		json.Unmarshal(input, &in)
		var gh schema.Github
		if err := h.DB.Preload("GitProvider").First(&gh, "\"githubId\" = ?", in.GithubID).Error; err != nil {
			return nil, &trpcErr{"Github not found", "NOT_FOUND", 404}
		}
		return gh, nil
	}

	// GitLab
	r["gitlab.gitlabProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var providers []schema.Gitlab
		h.DB.Preload("GitProvider").
			Joins("JOIN git_provider ON git_provider.\"gitProviderId\" = gitlab.\"gitProviderId\"").
			Where("git_provider.\"organizationId\" = ?", member.OrganizationID).
			Where("gitlab.\"accessToken\" IS NOT NULL AND gitlab.\"refreshToken\" IS NOT NULL").
			Find(&providers)
		return providers, nil
	}

	r["gitlab.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ GitlabID string `json:"gitlabId"` }
		json.Unmarshal(input, &in)
		var gl schema.Gitlab
		if err := h.DB.Preload("GitProvider").First(&gl, "\"gitlabId\" = ?", in.GitlabID).Error; err != nil {
			return nil, &trpcErr{"Gitlab not found", "NOT_FOUND", 404}
		}
		return gl, nil
	}

	// Gitea
	r["gitea.giteaProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var providers []schema.Gitea
		h.DB.Preload("GitProvider").
			Joins("JOIN git_provider ON git_provider.\"gitProviderId\" = gitea.\"gitProviderId\"").
			Where("git_provider.\"organizationId\" = ?", member.OrganizationID).
			Where("gitea.\"clientId\" IS NOT NULL AND gitea.\"clientSecret\" IS NOT NULL").
			Find(&providers)
		return providers, nil
	}

	r["gitea.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ GiteaID string `json:"giteaId"` }
		json.Unmarshal(input, &in)
		var gt schema.Gitea
		if err := h.DB.Preload("GitProvider").First(&gt, "\"giteaId\" = ?", in.GiteaID).Error; err != nil {
			return nil, &trpcErr{"Gitea not found", "NOT_FOUND", 404}
		}
		return gt, nil
	}

	// Bitbucket
	r["bitbucket.bitbucketProviders"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var providers []schema.Bitbucket
		h.DB.Preload("GitProvider").
			Joins("JOIN git_provider ON git_provider.\"gitProviderId\" = bitbucket.\"gitProviderId\"").
			Where("git_provider.\"organizationId\" = ?", member.OrganizationID).
			Find(&providers)
		return providers, nil
	}

	r["bitbucket.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ BitbucketID string `json:"bitbucketId"` }
		json.Unmarshal(input, &in)
		var bb schema.Bitbucket
		if err := h.DB.Preload("GitProvider").First(&bb, "\"bitbucketId\" = ?", in.BitbucketID).Error; err != nil {
			return nil, &trpcErr{"Bitbucket not found", "NOT_FOUND", 404}
		}
		return bb, nil
	}

	// Preview Deployments
	r["previewDeployment.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		var previews []schema.PreviewDeployment
		h.DB.Preload("Deployments", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"createdAt\" DESC").Limit(10)
		}).Preload("Domains").
			Where("\"applicationId\" = ?", in.ApplicationID).
			Order("\"createdAt\" DESC").
			Find(&previews)
		return previews, nil
	}

	r["previewDeployment.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ PreviewDeploymentID string `json:"previewDeploymentId"` }
		json.Unmarshal(input, &in)
		var preview schema.PreviewDeployment
		err := h.DB.Preload("Application").Preload("Deployments").Preload("Domains").
			First(&preview, "\"previewDeploymentId\" = ?", in.PreviewDeploymentID).Error
		if err != nil {
			return nil, &trpcErr{"Preview not found", "NOT_FOUND", 404}
		}
		return preview, nil
	}

	r["previewDeployment.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ PreviewDeploymentID string `json:"previewDeploymentId"` }
		json.Unmarshal(input, &in)
		if h.PreviewSvc != nil {
			h.PreviewSvc.RemovePreviewDeployment(in.PreviewDeploymentID)
		}
		return true, nil
	}
}

// registerDatabaseTRPC registers generic CRUD procedures for database services.
func (h *Handler) registerDatabaseTRPC(r procedureRegistry, routerName, modelPrefix, idField string) {
	tableName := strings.ToLower(modelPrefix)
	quotedID := fmt.Sprintf("\"%s\"", idField)

	r[routerName+".one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in[idField].(string)

		var result map[string]interface{}
		err := h.DB.Table(tableName).
			Where(quotedID+" = ?", id).
			First(&result).Error
		if err != nil {
			return nil, &trpcErr{modelPrefix + " not found", "NOT_FOUND", 404}
		}
		return result, nil
	}

	r[routerName+".remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in[idField].(string)
		h.DB.Table(tableName).Where(quotedID+" = ?", id).Delete(nil)
		return true, nil
	}

	r[routerName+".update"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in[idField].(string)
		delete(in, idField)
		h.DB.Table(tableName).Where(quotedID+" = ?", id).Updates(in)
		return true, nil
	}

	r[routerName+".deploy"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		// Database deploy = restart the container
		return true, nil
	}

	r[routerName+".start"] = r[routerName+".deploy"]
	r[routerName+".stop"] = r[routerName+".deploy"]
	r[routerName+".reload"] = r[routerName+".deploy"]
	r[routerName+".rebuild"] = r[routerName+".deploy"]

	r[routerName+".saveEnvironment"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in[idField].(string)
		env, _ := in["env"].(string)
		h.DB.Table(tableName).Where(quotedID+" = ?", id).Update("env", env)
		return true, nil
	}

	r[routerName+".saveExternalPort"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in[idField].(string)
		port := in["externalPort"]
		h.DB.Table(tableName).Where(quotedID+" = ?", id).Update("externalPort", port)
		return true, nil
	}

	r[routerName+".changeStatus"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in map[string]interface{}
		json.Unmarshal(input, &in)
		id, _ := in[idField].(string)
		status, _ := in["applicationStatus"].(string)
		h.DB.Table(tableName).Where(quotedID+" = ?", id).Update("applicationStatus", status)
		return true, nil
	}
}

// registerSimpleCRUDTRPC registers procedures for simpler CRUD routers.
func (h *Handler) registerSimpleCRUDTRPC(r procedureRegistry, getDefaultMember func(echo.Context) (*schema.Member, error)) {
	// Certificate
	r["certificates.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var certs []schema.Certificate
		h.DB.Find(&certs)
		return certs, nil
	}
	r["certificates.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ CertificateID string `json:"certificateId"` }
		json.Unmarshal(input, &in)
		var cert schema.Certificate
		if err := h.DB.First(&cert, "\"certificateId\" = ?", in.CertificateID).Error; err != nil {
			return nil, &trpcErr{"Certificate not found", "NOT_FOUND", 404}
		}
		return cert, nil
	}
	r["certificates.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var cert schema.Certificate
		json.Unmarshal(input, &cert)
		if err := h.DB.Create(&cert).Error; err != nil {
			return nil, err
		}
		return cert, nil
	}
	r["certificates.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ CertificateID string `json:"certificateId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Certificate{}, "\"certificateId\" = ?", in.CertificateID)
		return true, nil
	}

	// SSH Key
	r["sshKey.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var keys []schema.SSHKey
		h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&keys)
		return keys, nil
	}
	r["sshKey.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ SSHKeyID string `json:"sshKeyId"` }
		json.Unmarshal(input, &in)
		var key schema.SSHKey
		if err := h.DB.First(&key, "\"sshKeyId\" = ?", in.SSHKeyID).Error; err != nil {
			return nil, &trpcErr{"SSH Key not found", "NOT_FOUND", 404}
		}
		return key, nil
	}
	r["sshKey.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var key schema.SSHKey
		json.Unmarshal(input, &key)
		key.OrganizationID = member.OrganizationID
		if err := h.DB.Create(&key).Error; err != nil {
			return nil, err
		}
		return key, nil
	}
	r["sshKey.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ SSHKeyID string `json:"sshKeyId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.SSHKey{}, "\"sshKeyId\" = ?", in.SSHKeyID)
		return true, nil
	}

	// Registry
	r["registry.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var registries []schema.Registry
		h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&registries)
		return registries, nil
	}
	r["registry.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ RegistryID string `json:"registryId"` }
		json.Unmarshal(input, &in)
		var reg schema.Registry
		if err := h.DB.First(&reg, "\"registryId\" = ?", in.RegistryID).Error; err != nil {
			return nil, &trpcErr{"Registry not found", "NOT_FOUND", 404}
		}
		return reg, nil
	}
	r["registry.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var reg schema.Registry
		json.Unmarshal(input, &reg)
		reg.OrganizationID = member.OrganizationID
		if err := h.DB.Create(&reg).Error; err != nil {
			return nil, err
		}
		return reg, nil
	}
	r["registry.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ RegistryID string `json:"registryId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Registry{}, "\"registryId\" = ?", in.RegistryID)
		return true, nil
	}

	// Destination
	r["destination.all"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		member, err := getDefaultMember(c)
		if err != nil {
			return nil, err
		}
		var dests []schema.Destination
		h.DB.Where("\"organizationId\" = ?", member.OrganizationID).Find(&dests)
		return dests, nil
	}

	// Backup
	r["backup.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ BackupID string `json:"backupId"` }
		json.Unmarshal(input, &in)
		var b schema.Backup
		if err := h.DB.First(&b, "\"backupId\" = ?", in.BackupID).Error; err != nil {
			return nil, &trpcErr{"Backup not found", "NOT_FOUND", 404}
		}
		return b, nil
	}

	// Environment
	r["environment.byProjectId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ProjectID string `json:"projectId"` }
		json.Unmarshal(input, &in)
		var envs []schema.Environment
		h.DB.Where("\"projectId\" = ?", in.ProjectID).Find(&envs)
		return envs, nil
	}

	r["environment.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ EnvironmentID string `json:"environmentId"` }
		json.Unmarshal(input, &in)
		var env schema.Environment
		if err := h.DB.
			Preload("Applications").
			Preload("Composes").
			First(&env, "\"environmentId\" = ?", in.EnvironmentID).Error; err != nil {
			return nil, &trpcErr{"Environment not found", "NOT_FOUND", 404}
		}
		return env, nil
	}

	r["environment.create"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var env schema.Environment
		json.Unmarshal(input, &env)
		if err := h.DB.Create(&env).Error; err != nil {
			return nil, err
		}
		return env, nil
	}

	r["environment.remove"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ EnvironmentID string `json:"environmentId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Environment{}, "\"environmentId\" = ?", in.EnvironmentID)
		return true, nil
	}

	// Rollback
	r["rollback.rollback"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ RollbackID string `json:"rollbackId"` }
		json.Unmarshal(input, &in)
		var rb schema.Rollback
		if err := h.DB.First(&rb, "\"rollbackId\" = ?", in.RollbackID).Error; err != nil {
			return nil, &trpcErr{"Rollback not found", "NOT_FOUND", 404}
		}
		if h.Queue != nil && rb.ApplicationID != "" {
			title := fmt.Sprintf("Rollback to %s", rb.DockerImage)
			h.Queue.EnqueueDeployApplication(rb.ApplicationID, &title, nil)
		}
		return true, nil
	}

	r["rollback.delete"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ RollbackID string `json:"rollbackId"` }
		json.Unmarshal(input, &in)
		h.DB.Delete(&schema.Rollback{}, "\"rollbackId\" = ?", in.RollbackID)
		return true, nil
	}

	// Docker
	r["docker.getContainers"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		if h.Docker == nil {
			return []interface{}{}, nil
		}
		containers, err := h.Docker.ListContainers(c.Request().Context())
		if err != nil {
			return nil, err
		}
		return containers, nil
	}

	// Schedule
	r["schedule.list"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID           string `json:"id"`
			ScheduleType string `json:"scheduleType"`
		}
		json.Unmarshal(input, &in)
		var schedules []schema.Schedule
		query := h.DB.DB
		if in.ScheduleType == "application" {
			query = query.Where("\"applicationId\" = ?", in.ID)
		} else if in.ScheduleType == "compose" {
			query = query.Where("\"composeId\" = ?", in.ID)
		} else if in.ScheduleType == "server" {
			query = query.Where("\"serverId\" = ?", in.ID)
		}
		query.Find(&schedules)
		return schedules, nil
	}

	r["schedule.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ScheduleID string `json:"scheduleId"` }
		json.Unmarshal(input, &in)
		var s schema.Schedule
		if err := h.DB.First(&s, "\"scheduleId\" = ?", in.ScheduleID).Error; err != nil {
			return nil, &trpcErr{"Schedule not found", "NOT_FOUND", 404}
		}
		return s, nil
	}

	// Volume Backups
	r["volumeBackups.list"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct {
			ID               string `json:"id"`
			VolumeBackupType string `json:"volumeBackupType"`
		}
		json.Unmarshal(input, &in)
		var vbs []schema.VolumeBackup
		query := h.DB.DB
		if in.VolumeBackupType == "application" {
			query = query.Where("\"applicationId\" = ?", in.ID)
		} else if in.VolumeBackupType == "compose" {
			query = query.Where("\"composeId\" = ?", in.ID)
		}
		query.Find(&vbs)
		return vbs, nil
	}

	// Mounts
	r["mounts.allNamedByApplicationId"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ ApplicationID string `json:"applicationId"` }
		json.Unmarshal(input, &in)
		var mounts []schema.Mount
		h.DB.Where("\"applicationId\" = ?", in.ApplicationID).Find(&mounts)
		return mounts, nil
	}

	// Security
	r["security.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ SecurityID string `json:"securityId"` }
		json.Unmarshal(input, &in)
		var s schema.Security
		if err := h.DB.First(&s, "\"securityId\" = ?", in.SecurityID).Error; err != nil {
			return nil, &trpcErr{"Security not found", "NOT_FOUND", 404}
		}
		return s, nil
	}

	// Redirects
	r["redirects.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ RedirectID string `json:"redirectId"` }
		json.Unmarshal(input, &in)
		var rd schema.Redirect
		if err := h.DB.First(&rd, "\"redirectId\" = ?", in.RedirectID).Error; err != nil {
			return nil, &trpcErr{"Redirect not found", "NOT_FOUND", 404}
		}
		return rd, nil
	}

	// Port
	r["port.one"] = func(c echo.Context, input json.RawMessage) (interface{}, error) {
		var in struct{ PortID string `json:"portId"` }
		json.Unmarshal(input, &in)
		var p schema.Port
		if err := h.DB.First(&p, "\"portId\" = ?", in.PortID).Error; err != nil {
			return nil, &trpcErr{"Port not found", "NOT_FOUND", 404}
		}
		return p, nil
	}
}
