package ws

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"

	"github.com/dokploy/dokploy/internal/auth"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/docker"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
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

// DeploymentLogs streams deployment log file content over WebSocket.
func (h *Handler) DeploymentLogs(c echo.Context) error {
	_, err := h.authenticate(c.Request())
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	logPath := c.QueryParam("logPath")
	if logPath == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "logPath is required")
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	file, err := os.Open(logPath)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error opening log: %v", err)))
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := conn.WriteMessage(websocket.TextMessage, scanner.Bytes()); err != nil {
			break
		}
	}

	// Watch for new content (tail -f behavior)
	// TODO: Use fsnotify or polling for real-time updates

	return nil
}

// ContainerLogs streams Docker container logs over WebSocket.
func (h *Handler) ContainerLogs(c echo.Context) error {
	_, err := h.authenticate(c.Request())
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	containerID := c.QueryParam("containerId")
	tail := c.QueryParam("tail")
	if tail == "" {
		tail = "100"
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Listen for client disconnect
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

// DockerStats streams Docker stats over WebSocket.
func (h *Handler) DockerStats(c echo.Context) error {
	_, err := h.authenticate(c.Request())
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	// TODO: Stream docker stats using Docker SDK
	conn.WriteMessage(websocket.TextMessage, []byte(`{"message":"docker stats not yet implemented"}`))
	return nil
}

// Terminal provides a WebSocket-based terminal session.
func (h *Handler) Terminal(c echo.Context) error {
	_, err := h.authenticate(c.Request())
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	containerID := c.QueryParam("containerId")
	if containerID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "containerId is required")
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Use docker exec to create an interactive session
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", "-it", containerID, "/bin/sh")

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

	cmd.Stderr = cmd.Stdout // Combine stderr with stdout

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
