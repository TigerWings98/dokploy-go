// Input: gorilla/websocket, docker SDK, process/ssh
// Output: 5 个 WebSocket 端点 (DeploymentLogs/ContainerLogs/DockerStats/Terminal/ServerTerminal)
// Role: WebSocket 服务核心，提供实时日志流/容器统计/交互式终端等双向通信功能
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package ws

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/creack/pty"
	"github.com/dokploy/dokploy/internal/auth"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/docker"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/ssh"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:  func(r *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// Handler holds WebSocket handler dependencies.
type Handler struct {
	DB             *db.DB
	Docker         *docker.Client
	Auth           *auth.Auth
	MonitoringPath string
}

// NewHandler creates a new WebSocket handler.
func NewHandler(database *db.DB, dockerClient *docker.Client, a *auth.Auth, monitoringPath string) *Handler {
	return &Handler{DB: database, Docker: dockerClient, Auth: a, MonitoringPath: monitoringPath}
}

// RegisterRoutes registers WebSocket routes.
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	e.GET("/ws/deployment-logs", h.DeploymentLogs)
	e.GET("/ws/container-logs", h.ContainerLogs)
	e.GET("/ws/docker-stats", h.DockerStats)
	e.GET("/ws/terminal", h.Terminal)

	// Server terminal (SSH shell)
	e.GET("/ws/server-terminal", h.ServerTerminal)
	e.GET("/terminal", h.ServerTerminal)

	// tRPC WebSocket subscriptions (drawer logs)
	e.GET("/drawer-logs", h.DrawerLogs)

	// Frontend compatibility aliases (original Dokploy paths)
	e.GET("/listen-deployment", h.DeploymentLogs)
	e.GET("/docker-container-logs", h.ContainerLogs)
	e.GET("/docker-container-terminal", h.Terminal)
	e.GET("/listen-docker-stats-monitoring", h.DockerStatsMonitoring)
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

	serverId := c.QueryParam("serverId")

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 检测客户端断开
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// 远程服务器：通过 SSH tail -f 读取日志
	if serverId != "" {
		h.deploymentLogsRemote(conn, ctx, serverId, cleanPath)
		return nil
	}

	// 本地：直接读取文件
	h.deploymentLogsLocal(conn, ctx, cleanPath)
	return nil
}

// deploymentLogsLocal 从本地文件系统读取日志并流式发送
func (h *Handler) deploymentLogsLocal(conn *websocket.Conn, ctx context.Context, logPath string) {
	file, err := os.Open(logPath)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error opening log: %v", err)))
		return
	}
	defer file.Close()

	// 读取已有内容
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4096)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := append(scanner.Bytes(), '\n')
		if err := conn.WriteMessage(websocket.TextMessage, line); err != nil {
			return
		}
	}

	// 轮询新内容 (tail -f 行为)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				n, readErr := file.Read(buf)
				if n > 0 {
					if writeErr := conn.WriteMessage(websocket.TextMessage, buf[:n]); writeErr != nil {
						return
					}
				}
				if readErr != nil {
					break
				}
			}
		}
	}
}

// deploymentLogsRemote 通过 SSH 在远程服务器上执行 tail -f 读取日志
func (h *Handler) deploymentLogsRemote(conn *websocket.Conn, ctx context.Context, serverId, logPath string) {
	var server schema.Server
	if err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", serverId).Error; err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Server not found: %v\n", err)))
		return
	}
	if server.SSHKey == nil {
		conn.WriteMessage(websocket.TextMessage, []byte("No SSH key configured for this server\n"))
		return
	}

	signer, err := ssh.ParsePrivateKey([]byte(server.SSHKey.PrivateKey))
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH key error: %v\n", err)))
		return
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", server.IPAddress, server.Port), &ssh.ClientConfig{
		User:            server.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH connection error: %v\n", err)))
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH session error: %v\n", err)))
		return
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH pipe error: %v\n", err)))
		return
	}

	// 在远程服务器上执行 tail -n +1 -f（与 TS 版一致）
	if err := session.Start(fmt.Sprintf("tail -n +1 -f %s", logPath)); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Command error: %v\n", err)))
		return
	}

	// 流式读取远程输出并发送到 WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.TextMessage, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// 等待客户端断开
	<-ctx.Done()
	session.Signal(ssh.SIGTERM)
}

// --- Container Logs ---
// Uses CLI `docker container logs --timestamps --follow` to match original TypeScript.
// The Docker SDK returns a multiplexed binary stream with 8-byte headers that
// breaks the frontend timestamp parser. CLI output is plain text with timestamps.

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

	search := c.QueryParam("search")
	if search != "" && !reSearch.MatchString(search) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid search value")
	}

	runType := c.QueryParam("runType")
	serverId := c.QueryParam("serverId")

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// Build command: docker container logs or docker service logs
	dockerCmd := "container"
	if runType == "swarm" {
		dockerCmd = "service"
	}

	args := []string{dockerCmd, "logs", "--timestamps"}
	if runType == "swarm" {
		args = append(args, "--raw")
	}
	args = append(args, "--tail", tail)
	if since != "" && since != "all" {
		args = append(args, "--since", since)
	}
	args = append(args, "--follow", containerID)

	if serverId != "" {
		// Remote server: use SSH
		h.streamLogsViaSSH(ctx, conn, serverId, args, search)
	} else {
		// Local: run docker CLI directly
		h.streamLogsLocal(ctx, conn, args, search)
	}

	return nil
}

func (h *Handler) streamLogsLocal(ctx context.Context, conn *websocket.Conn, args []string, search string) {
	// Always use shell to merge stderr (docker logs outputs to stderr) via 2>&1
	shellCmd := "docker " + strings.Join(args, " ") + " 2>&1"
	if search != "" {
		shellCmd += fmt.Sprintf(" | grep --line-buffered -iF '%s'",
			strings.ReplaceAll(search, "'", "'\\''"))
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", shellCmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v", err)))
		return
	}

	if err := cmd.Start(); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v", err)))
		return
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		line := append(scanner.Bytes(), '\n')
		if err := conn.WriteMessage(websocket.TextMessage, line); err != nil {
			break
		}
	}
	cmd.Wait()
}

func (h *Handler) streamLogsViaSSH(ctx context.Context, conn *websocket.Conn, serverId string, args []string, search string) {
	var server schema.Server
	if err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", serverId).Error; err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Server not found: %v", err)))
		return
	}
	if server.SSHKey == nil {
		conn.WriteMessage(websocket.TextMessage, []byte("No SSH key for server"))
		return
	}

	signer, err := ssh.ParsePrivateKey([]byte(server.SSHKey.PrivateKey))
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH key error: %v", err)))
		return
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", server.IPAddress, server.Port), &ssh.ClientConfig{
		User:            server.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH connect error: %v", err)))
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH session error: %v", err)))
		return
	}
	defer session.Close()

	// Request PTY so docker logs gets SIGHUP on disconnect
	session.RequestPty("xterm", 80, 30, ssh.TerminalModes{})

	remoteCmd := "docker " + strings.Join(args, " ")
	if search != "" {
		remoteCmd += fmt.Sprintf(" 2>&1 | grep --line-buffered -iF '%s'",
			strings.ReplaceAll(search, "'", "'\\''"))
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		return
	}

	if err := session.Start(remoteCmd); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v", err)))
		return
	}

	go func() {
		<-ctx.Done()
		session.Close()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		line := append(scanner.Bytes(), '\n')
		if err := conn.WriteMessage(websocket.TextMessage, line); err != nil {
			break
		}
	}
	session.Wait()
}

// --- Docker Stats ---

func (h *Handler) DockerStats(c echo.Context) error {
	return h.DockerStatsMonitoring(c)
}

// DockerStatsMonitoring implements the /listen-docker-stats-monitoring endpoint.
// It polls docker stats every 1.3s, records to JSON files, and sends the last
// value over WebSocket as {data: {cpu, memory, block, network, disk}}.
func (h *Handler) DockerStatsMonitoring(c echo.Context) error {
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

	appType := c.QueryParam("appType")
	if appType == "" {
		appType = "application"
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	ticker := time.NewTicker(1300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			rawStats, err := h.getDockerRawStats(ctx, appName, appType)
			if err != nil {
				conn.WriteMessage(websocket.TextMessage, []byte(`{"data":{}}`))
				continue
			}

			h.recordStats(appName, rawStats)

			lastData := h.getLastStats(appName)
			msg, _ := json.Marshal(map[string]interface{}{"data": lastData})
			if writeErr := conn.WriteMessage(websocket.TextMessage, msg); writeErr != nil {
				return nil
			}
		}
	}
}

type statEntry struct {
	Value interface{} `json:"value"`
	Time  string      `json:"time"`
}

func (h *Handler) getDockerRawStats(ctx context.Context, appName, appType string) (map[string]string, error) {
	// For dokploy host monitoring, use system stats
	if appName == "dokploy" {
		return h.getHostStats(ctx)
	}

	// Find container by label or name depending on appType
	var filterArg string
	switch appType {
	case "application":
		filterArg = fmt.Sprintf("label=com.docker.swarm.service.name=%s", appName)
	case "stack":
		filterArg = fmt.Sprintf("label=com.docker.swarm.task.name=%s", appName)
	default: // docker-compose
		filterArg = fmt.Sprintf("name=%s", appName)
	}

	// Find running container
	findCmd := exec.CommandContext(ctx, "docker", "ps", "-q", "--filter", "status=running", "--filter", filterArg)
	containerOut, err := findCmd.Output()
	if err != nil || strings.TrimSpace(string(containerOut)) == "" {
		// Fallback: try name filter
		findCmd = exec.CommandContext(ctx, "docker", "ps", "-q", "--filter", "status=running", "--filter", fmt.Sprintf("name=%s", appName))
		containerOut, err = findCmd.Output()
		if err != nil || strings.TrimSpace(string(containerOut)) == "" {
			return nil, fmt.Errorf("container not found")
		}
	}

	containerID := strings.Split(strings.TrimSpace(string(containerOut)), "\n")[0]

	cmd := exec.CommandContext(ctx, "docker", "stats", containerID, "--no-stream",
		"--format", `{{.CPUPerc}}\t{{.MemUsage}}\t{{.BlockIO}}\t{{.NetIO}}`)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	line := strings.TrimSpace(string(output))
	if line == "" {
		return nil, fmt.Errorf("no stats")
	}
	parts := strings.Split(strings.Split(line, "\n")[0], "\t")
	if len(parts) < 4 {
		return nil, fmt.Errorf("unexpected format")
	}

	return map[string]string{
		"CPUPerc":  parts[0],
		"MemUsage": parts[1],
		"BlockIO":  parts[2],
		"NetIO":    parts[3],
	}, nil
}

func (h *Handler) getHostStats(ctx context.Context) (map[string]string, error) {
	// CPU usage
	cpuCmd := exec.CommandContext(ctx, "sh", "-c",
		`top -bn1 | grep "Cpu(s)" | awk '{print $2+$4"%"}'`)
	cpuOut, _ := cpuCmd.Output()
	cpuPerc := strings.TrimSpace(string(cpuOut))
	if cpuPerc == "" {
		cpuPerc = "0%"
	}

	// Memory
	memCmd := exec.CommandContext(ctx, "sh", "-c",
		`free -b | awk '/^Mem:/{printf "%.2fGiB / %.2fGiB", $3/1073741824, $2/1073741824}'`)
	memOut, _ := memCmd.Output()
	memUsage := strings.TrimSpace(string(memOut))
	if memUsage == "" {
		memUsage = "0GiB / 0GiB"
	}

	// Disk (BlockIO placeholder, disk stats below)
	blockIO := "0B / 0B"

	// Network
	netCmd := exec.CommandContext(ctx, "sh", "-c",
		`cat /proc/net/dev | awk 'NR>2{rx+=$2;tx+=$10}END{printf "%.2fMB / %.2fMB", rx/1048576, tx/1048576}'`)
	netOut, _ := netCmd.Output()
	netIO := strings.TrimSpace(string(netOut))
	if netIO == "" {
		netIO = "0MB / 0MB"
	}

	return map[string]string{
		"CPUPerc":  cpuPerc,
		"MemUsage": memUsage,
		"BlockIO":  blockIO,
		"NetIO":    netIO,
	}, nil
}

func (h *Handler) recordStats(appName string, raw map[string]string) {
	if h.MonitoringPath == "" {
		return
	}
	dir := filepath.Join(h.MonitoringPath, appName)
	os.MkdirAll(dir, 0755)

	now := time.Now().UTC().Format(time.RFC3339)

	// CPU: value is the percentage string
	h.appendStatFile(dir, "cpu", statEntry{Value: raw["CPUPerc"], Time: now})

	// Memory: split "used / total"
	memParts := strings.Split(raw["MemUsage"], " / ")
	memVal := map[string]string{"used": "0", "total": "0"}
	if len(memParts) >= 2 {
		memVal["used"] = strings.TrimSpace(memParts[0])
		memVal["total"] = strings.TrimSpace(memParts[1])
	}
	h.appendStatFile(dir, "memory", statEntry{Value: memVal, Time: now})

	// Block I/O: split "read / write"
	blockParts := strings.Split(raw["BlockIO"], " / ")
	blockVal := map[string]string{"readMb": "0", "writeMb": "0"}
	if len(blockParts) >= 2 {
		blockVal["readMb"] = strings.TrimSpace(blockParts[0])
		blockVal["writeMb"] = strings.TrimSpace(blockParts[1])
	}
	h.appendStatFile(dir, "block", statEntry{Value: blockVal, Time: now})

	// Network I/O: split "in / out"
	netParts := strings.Split(raw["NetIO"], " / ")
	netVal := map[string]string{"inputMb": "0", "outputMb": "0"}
	if len(netParts) >= 2 {
		netVal["inputMb"] = strings.TrimSpace(netParts[0])
		netVal["outputMb"] = strings.TrimSpace(netParts[1])
	}
	h.appendStatFile(dir, "network", statEntry{Value: netVal, Time: now})

	// Disk (only for dokploy)
	if appName == "dokploy" {
		diskVal := h.getDiskStats()
		h.appendStatFile(dir, "disk", statEntry{Value: diskVal, Time: now})
	}
}

func (h *Handler) getDiskStats() map[string]interface{} {
	cmd := exec.Command("df", "-B1", "/")
	out, err := cmd.Output()
	if err != nil {
		return map[string]interface{}{"diskTotal": 0, "diskUsage": 0, "diskUsedPercentage": 0, "diskFree": 0}
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return map[string]interface{}{"diskTotal": 0, "diskUsage": 0, "diskUsedPercentage": 0, "diskFree": 0}
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return map[string]interface{}{"diskTotal": 0, "diskUsage": 0, "diskUsedPercentage": 0, "diskFree": 0}
	}
	total, _ := strconv.ParseFloat(fields[1], 64)
	used, _ := strconv.ParseFloat(fields[2], 64)
	free, _ := strconv.ParseFloat(fields[3], 64)
	gb := 1024.0 * 1024.0 * 1024.0
	pct := 0.0
	if total > 0 {
		pct = (used / total) * 100
	}
	return map[string]interface{}{
		"diskTotal":          fmt.Sprintf("%.2f", total/gb),
		"diskUsage":          fmt.Sprintf("%.2f", used/gb),
		"diskUsedPercentage": fmt.Sprintf("%.2f", pct),
		"diskFree":           fmt.Sprintf("%.2f", free/gb),
	}
}

func (h *Handler) appendStatFile(dir, statType string, entry statEntry) {
	filePath := filepath.Join(dir, statType+".json")
	var entries []statEntry

	data, err := os.ReadFile(filePath)
	if err == nil {
		json.Unmarshal(data, &entries)
	}

	entries = append(entries, entry)
	if len(entries) > 288 {
		entries = entries[len(entries)-288:]
	}

	out, _ := json.Marshal(entries)
	os.WriteFile(filePath, out, 0644)
}

func (h *Handler) getLastStats(appName string) map[string]interface{} {
	result := map[string]interface{}{
		"cpu":     nil,
		"memory":  nil,
		"block":   nil,
		"network": nil,
		"disk":    nil,
	}
	if h.MonitoringPath == "" {
		return result
	}
	dir := filepath.Join(h.MonitoringPath, appName)
	for _, statType := range []string{"cpu", "memory", "block", "network", "disk"} {
		data, err := os.ReadFile(filepath.Join(dir, statType+".json"))
		if err != nil {
			continue
		}
		var entries []statEntry
		if json.Unmarshal(data, &entries) == nil && len(entries) > 0 {
			result[statType] = entries[len(entries)-1]
		}
	}
	return result
}

// ReadAllStats reads all stats for readAppMonitoring tRPC endpoint.
func ReadAllStats(monitoringPath, appName string) map[string]interface{} {
	result := map[string]interface{}{
		"cpu":     []interface{}{},
		"memory":  []interface{}{},
		"block":   []interface{}{},
		"network": []interface{}{},
		"disk":    []interface{}{},
	}
	if monitoringPath == "" {
		return result
	}
	dir := filepath.Join(monitoringPath, appName)
	for _, statType := range []string{"cpu", "memory", "block", "network", "disk"} {
		data, err := os.ReadFile(filepath.Join(dir, statType+".json"))
		if err != nil {
			continue
		}
		var entries []json.RawMessage
		if json.Unmarshal(data, &entries) == nil {
			ifaces := make([]interface{}, len(entries))
			for i, e := range entries {
				var v interface{}
				json.Unmarshal(e, &v)
				ifaces[i] = v
			}
			result[statType] = ifaces
		}
	}
	return result
}

// --- Docker Container Terminal ---
// Uses creack/pty for proper PTY allocation so xterm.js works correctly.
// Frontend sends `activeWay` param (bash/sh), not `shell`.

func (h *Handler) Terminal(c echo.Context) error {
	_, err := h.authenticate(c.Request())
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	containerID := c.QueryParam("containerId")
	if !isValidContainerID(containerID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid containerId")
	}

	// Frontend sends "activeWay" param, fall back to "shell" for compatibility
	shell := c.QueryParam("activeWay")
	if shell == "" {
		shell = c.QueryParam("shell")
	}
	if shell == "" {
		shell = "sh"
	}
	if !allowedShells[shell] {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid shell")
	}

	serverId := c.QueryParam("serverId")

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if serverId != "" {
		// Remote server: SSH + docker exec
		h.dockerTerminalViaSSH(conn, serverId, containerID, shell)
	} else {
		// Local: docker exec with PTY
		h.dockerTerminalLocal(conn, containerID, shell)
	}

	return nil
}

func (h *Handler) dockerTerminalLocal(conn *websocket.Conn, containerID, shell string) {
	cmd := exec.Command("docker", "exec", "-it", "-w", "/", containerID, shell)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Failed to start terminal: %v", err)))
		return
	}
	defer ptmx.Close()

	// PTY → WebSocket
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					break
				}
			}
			if readErr != nil {
				break
			}
		}
	}()

	// WebSocket → PTY
	for {
		_, msg, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		if _, writeErr := ptmx.Write(msg); writeErr != nil {
			break
		}
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func (h *Handler) dockerTerminalViaSSH(conn *websocket.Conn, serverId, containerID, shell string) {
	var server schema.Server
	if err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", serverId).Error; err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Server not found: %v", err)))
		return
	}
	if server.SSHKey == nil {
		conn.WriteMessage(websocket.TextMessage, []byte("No SSH key configured for this server"))
		return
	}

	signer, err := ssh.ParsePrivateKey([]byte(server.SSHKey.PrivateKey))
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH key error: %v", err)))
		return
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", server.IPAddress, server.Port), &ssh.ClientConfig{
		User:            server.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH error: %v", err)))
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH session error: %v", err)))
		return
	}
	defer session.Close()

	session.RequestPty("xterm", 30, 80, ssh.TerminalModes{
		ssh.ECHO: 1,
	})

	stdin, _ := session.StdinPipe()
	stdout, _ := session.StdoutPipe()

	remoteCmd := fmt.Sprintf("docker exec -it -w / %s %s", containerID, shell)
	if err := session.Start(remoteCmd); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v", err)))
		return
	}

	// SSH stdout → WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					break
				}
			}
			if readErr != nil {
				break
			}
		}
	}()

	// WebSocket → SSH stdin
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
	session.Wait()
}

// --- Server Terminal (SSH) ---
// /terminal?serverId=X - Opens SSH shell to server.
// For local server (serverId=local): uses auto-generated SSH key at /etc/dokploy/ssh/
// For remote server: uses server's SSH key from DB.

func (h *Handler) ServerTerminal(c echo.Context) error {
	_, err := h.authenticate(c.Request())
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	serverId := c.QueryParam("serverId")
	if serverId == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "serverId is required")
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if serverId == "local" {
		port := c.QueryParam("port")
		username := c.QueryParam("username")
		if port == "" || username == "" {
			conn.WriteMessage(websocket.TextMessage, []byte("port and username required for local server"))
			return nil
		}
		portInt, _ := strconv.Atoi(port)
		h.serverTerminalLocal(conn, portInt, username)
	} else {
		h.serverTerminalRemote(conn, serverId)
	}
	return nil
}

func (h *Handler) serverTerminalLocal(conn *websocket.Conn, port int, username string) {
	// Read auto-generated SSH key
	keyPath := "/etc/dokploy/ssh/auto_generated-dokploy-local"
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH key not found: %v\n", err)))
		return
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH key error: %v\n", err)))
		return
	}

	// Determine host: try Docker host detection, fall back to localhost
	host := "localhost"
	hostCmd := exec.Command("sh", "-c", `ip route show default | awk '{print $3}' | head -n1`)
	if out, err := hostCmd.Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		host = strings.TrimSpace(string(out))
	}

	conn.WriteMessage(websocket.TextMessage, []byte("Connecting...\n"))

	h.sshShellSession(conn, host, port, username, signer)
}

func (h *Handler) serverTerminalRemote(conn *websocket.Conn, serverId string) {
	var server schema.Server
	if err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", serverId).Error; err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Server not found: %v\n", err)))
		return
	}
	if server.SSHKey == nil {
		conn.WriteMessage(websocket.TextMessage, []byte("No SSH key configured for this server\n"))
		return
	}

	signer, err := ssh.ParsePrivateKey([]byte(server.SSHKey.PrivateKey))
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH key error: %v\n", err)))
		return
	}

	conn.WriteMessage(websocket.TextMessage, []byte("Connecting...\n"))

	h.sshShellSession(conn, server.IPAddress, server.Port, server.Username, signer)
}

func (h *Handler) sshShellSession(conn *websocket.Conn, host string, port int, username string, signer ssh.Signer) {
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", host, port), &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH connect error: %v\n", err)))
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH session error: %v\n", err)))
		return
	}
	defer session.Close()

	session.RequestPty("xterm-256color", 30, 80, ssh.TerminalModes{
		ssh.ECHO: 1,
	})

	stdin, _ := session.StdinPipe()
	stdout, _ := session.StdoutPipe()

	if err := session.Shell(); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Shell error: %v\n", err)))
		return
	}

	// Clear terminal once connected
	conn.WriteMessage(websocket.TextMessage, []byte("\x1bc"))

	// SSH → WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					break
				}
			}
			if readErr != nil {
				break
			}
		}
	}()

	// WebSocket → SSH
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
	session.Wait()
}
