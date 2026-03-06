package ws

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/auth"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/docker"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:  func(r *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// Handler holds WebSocket handler dependencies.
type Handler struct {
	DB     *db.DB
	Docker *docker.Client
	Auth   *auth.Auth
}

// NewHandler creates a new WebSocket handler.
func NewHandler(database *db.DB, dockerClient *docker.Client, a *auth.Auth) *Handler {
	return &Handler{DB: database, Docker: dockerClient, Auth: a}
}

// RegisterRoutes registers WebSocket routes.
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	e.GET("/ws/deployment-logs", h.DeploymentLogs)
	e.GET("/ws/container-logs", h.ContainerLogs)
	e.GET("/ws/docker-stats", h.DockerStats)
	e.GET("/ws/terminal", h.Terminal)

	// Frontend compatibility aliases (original Dokploy paths)
	e.GET("/listen-deployment", h.DeploymentLogs)
}

// authenticate validates the WebSocket connection.
func (h *Handler) authenticate(r *http.Request) (*schema.User, error) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = auth.GetSessionTokenFromRequest(r)
	}
	if token == "" {
		return nil, fmt.Errorf("no authentication token provided")
	}

	user, _, err := h.Auth.ValidateSession(token)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// --- Input validation (prevent command injection) ---

var (
	reContainerID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	reTail        = regexp.MustCompile(`^[0-9]{1,5}$`)
	reSince       = regexp.MustCompile(`^(all|[0-9]+[smhd])$`)
	reSearch      = regexp.MustCompile(`^[a-zA-Z0-9 ._-]{0,500}$`)
	allowedShells = map[string]bool{
		"sh": true, "bash": true, "zsh": true, "ash": true,
		"/bin/sh": true, "/bin/bash": true, "/bin/zsh": true, "/bin/ash": true,
	}
)

func isValidContainerID(s string) bool { return s != "" && reContainerID.MatchString(s) }
func isValidTail(s string) bool        { return reTail.MatchString(s) }
func isValidSince(s string) bool       { return reSince.MatchString(s) }

// --- Deployment Logs (tail -f with polling) ---
// Memory note: Uses polling instead of fsnotify to avoid watcher goroutine leaks.
// Fixed 4KB read buffer, no accumulation. Goroutine exits when client disconnects.

func (h *Handler) DeploymentLogs(c echo.Context) error {
	_, err := h.authenticate(c.Request())
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	logPath := c.QueryParam("logPath")
	if logPath == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "logPath is required")
	}

	// Security: only allow paths under /etc/dokploy or /tmp
	cleanPath := filepath.Clean(logPath)
	if !strings.HasPrefix(cleanPath, "/etc/dokploy/") && !strings.HasPrefix(cleanPath, "/tmp/") {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid log path")
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Detect client disconnect
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	file, err := os.Open(cleanPath)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error opening log: %v", err)))
		return nil
	}
	defer file.Close()

	// Read existing content
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4096)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		if err := conn.WriteMessage(websocket.TextMessage, scanner.Bytes()); err != nil {
			return nil
		}
	}

	// Poll for new content (tail -f behavior)
	// Polling at 500ms is lighter than fsnotify for single-file watching
	// and avoids inotify descriptor leaks.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for {
				n, readErr := file.Read(buf)
				if n > 0 {
					if writeErr := conn.WriteMessage(websocket.TextMessage, buf[:n]); writeErr != nil {
						return nil
					}
				}
				if readErr != nil {
					break // EOF or error, wait for next tick
				}
			}
		}
	}
}

// --- Container Logs ---
// Memory note: Fixed 4KB buffer, streams directly from Docker daemon.
// No intermediate buffering. Context cancellation ensures clean shutdown.

func (h *Handler) ContainerLogs(c echo.Context) error {
	_, err := h.authenticate(c.Request())
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	containerID := c.QueryParam("containerId")
	if !isValidContainerID(containerID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid containerId")
	}

	tail := c.QueryParam("tail")
	if tail == "" {
		tail = "100"
	} else if !isValidTail(tail) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid tail value")
	}

	since := c.QueryParam("since")
	if since != "" && since != "all" && !isValidSince(since) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid since value")
	}

	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Detect client disconnect
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	reader, err := h.Docker.ContainerLogs(ctx, containerID, tail)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v", err)))
		return nil
	}
	defer reader.Close()

	// Stream with fixed buffer — no accumulation
	buf := make([]byte, 4096)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			if writeErr := conn.WriteMessage(websocket.TextMessage, buf[:n]); writeErr != nil {
				break
			}
		}
		if readErr != nil {
			break
		}
	}

	return nil
}

// --- Docker Stats ---
// Memory note: Polls every 1.3s (matching TS version), single JSON snapshot per poll.
// No streaming stats — each request is a one-shot read then close.
// This avoids the Docker stats stream leak that plagues long-running connections.

type containerStats struct {
	Name     string  `json:"name"`
	CPUPerc  float64 `json:"cpuPerc"`
	MemUsage uint64  `json:"memUsage"`
	MemLimit uint64  `json:"memLimit"`
	MemPerc  float64 `json:"memPerc"`
	NetIO    string  `json:"netIO"`
	BlockIO  string  `json:"blockIO"`
}

func (h *Handler) DockerStats(c echo.Context) error {
	_, err := h.authenticate(c.Request())
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	appName := c.QueryParam("appName")
	if appName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "appName is required")
	}
	if !isValidContainerID(appName) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid appName")
	}

	if h.Docker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Docker client not available")
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Detect client disconnect
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// Poll docker stats every 1.3 seconds
	ticker := time.NewTicker(1300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			stats, err := h.getContainerStats(ctx, appName)
			if err != nil {
				// Container might not be running yet, send empty
				conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"container not found"}`))
				continue
			}
			data, _ := json.Marshal(stats)
			if writeErr := conn.WriteMessage(websocket.TextMessage, data); writeErr != nil {
				return nil
			}
		}
	}
}

func (h *Handler) getContainerStats(ctx context.Context, appName string) (*containerStats, error) {
	// Use docker stats --no-stream for a single snapshot (no memory leak)
	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream",
		"--format", `{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}`,
		"--filter", fmt.Sprintf("name=%s", appName))

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	line := strings.TrimSpace(string(output))
	if line == "" {
		return nil, fmt.Errorf("no stats found")
	}

	// Take first line if multiple matches
	lines := strings.Split(line, "\n")
	parts := strings.Split(lines[0], "\t")
	if len(parts) < 6 {
		return nil, fmt.Errorf("unexpected stats format")
	}

	cpuStr := strings.TrimSuffix(parts[1], "%")
	cpuPerc, _ := strconv.ParseFloat(cpuStr, 64)

	memPercStr := strings.TrimSuffix(parts[3], "%")
	memPerc, _ := strconv.ParseFloat(memPercStr, 64)

	return &containerStats{
		Name:    parts[0],
		CPUPerc: cpuPerc,
		MemPerc: memPerc,
		NetIO:   parts[4],
		BlockIO: parts[5],
	}, nil
}

// --- Terminal ---
// Memory note: Fixed 4KB buffer each direction. Process killed on disconnect.
// Shell whitelist prevents command injection. No PTY library dependency
// (avoids node-pty's memory overhead).

func (h *Handler) Terminal(c echo.Context) error {
	_, err := h.authenticate(c.Request())
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	containerID := c.QueryParam("containerId")
	if !isValidContainerID(containerID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid containerId")
	}

	shell := c.QueryParam("shell")
	if shell == "" {
		shell = "/bin/sh"
	}
	if !allowedShells[shell] {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid shell")
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", containerID, shell)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("Failed to get stdin pipe: %v", err)
		return nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Failed to get stdout pipe: %v", err)
		return nil
	}

	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Failed to start terminal: %v", err)))
		return nil
	}

	// Read from container → send to WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					break
				}
			}
			if readErr == io.EOF || readErr != nil {
				break
			}
		}
		cancel()
	}()

	// Read from WebSocket → send to container
	for {
		_, msg, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		if _, writeErr := stdin.Write(msg); writeErr != nil {
			break
		}
	}

	stdin.Close()
	cmd.Wait()
	return nil
}
