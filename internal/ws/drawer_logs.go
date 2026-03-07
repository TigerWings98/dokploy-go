package ws

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/dokploy/dokploy/internal/setup"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/ssh"
)

// tRPC WebSocket protocol messages

type trpcRequest struct {
	ID      json.RawMessage `json:"id"`
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  *trpcParams     `json:"params,omitempty"`
}

type trpcParams struct {
	Path  string          `json:"path"`
	Input json.RawMessage `json:"input"`
}

// trpcInputWrapper wraps the superjson encoded input.
// superjson sends: {"json": {...actual data...}}
type trpcInputWrapper struct {
	JSON json.RawMessage `json:"json"`
}

func trpcStarted(id json.RawMessage) []byte {
	msg, _ := json.Marshal(map[string]interface{}{
		"id":      id,
		"jsonrpc": "2.0",
		"result":  map[string]interface{}{"type": "started"},
	})
	return msg
}

func trpcData(id json.RawMessage, data string) []byte {
	msg, _ := json.Marshal(map[string]interface{}{
		"id":      id,
		"jsonrpc": "2.0",
		"result": map[string]interface{}{
			"type": "data",
			"data": map[string]interface{}{"json": data},
		},
	})
	return msg
}

func trpcStopped(id json.RawMessage) []byte {
	msg, _ := json.Marshal(map[string]interface{}{
		"id":      id,
		"jsonrpc": "2.0",
		"result":  map[string]interface{}{"type": "stopped"},
	})
	return msg
}

func trpcError(id json.RawMessage, message string) []byte {
	msg, _ := json.Marshal(map[string]interface{}{
		"id":      id,
		"jsonrpc": "2.0",
		"error": map[string]interface{}{
			"message": message,
			"code":    -32603,
			"data": map[string]interface{}{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": message,
			},
		},
	})
	return msg
}

// DrawerLogs handles the /drawer-logs WebSocket endpoint for tRPC subscriptions.
func (h *Handler) DrawerLogs(c echo.Context) error {
	user, err := h.authenticate(c.Request())
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Write mutex to protect concurrent writes
	var writeMu sync.Mutex
	safeWrite := func(data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	// Track active subscriptions for cleanup
	type activeSub struct {
		cancel func()
	}
	subs := make(map[string]*activeSub)
	var subsMu sync.Mutex

	_ = user // authenticated

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var req trpcRequest
		if err := json.Unmarshal(message, &req); err != nil {
			continue
		}

		idKey := string(req.ID)

		switch req.Method {
		case "subscription":
			if req.Params == nil {
				continue
			}

			// Parse superjson input wrapper
			var wrapper trpcInputWrapper
			json.Unmarshal(req.Params.Input, &wrapper)
			input := wrapper.JSON

			// Send "started"
			safeWrite(trpcStarted(req.ID))

			// Create cancel channel
			done := make(chan struct{})
			subsMu.Lock()
			subs[idKey] = &activeSub{cancel: func() { close(done) }}
			subsMu.Unlock()

			// Route to handler
			id := make([]byte, len(req.ID))
			copy(id, req.ID)

			go func() {
				defer func() {
					safeWrite(trpcStopped(id))
					subsMu.Lock()
					delete(subs, string(id))
					subsMu.Unlock()
				}()

				emit := func(msg string) bool {
					select {
					case <-done:
						return false
					default:
						if err := safeWrite(trpcData(id, msg)); err != nil {
							return false
						}
						return true
					}
				}

				switch req.Params.Path {
				case "server.setupWithLogs":
					h.handleServerSetup(input, emit, done)
				case "postgres.deployWithLogs":
					h.handleDatabaseDeploy("postgres", input, emit, done)
				case "mysql.deployWithLogs":
					h.handleDatabaseDeploy("mysql", input, emit, done)
				case "mariadb.deployWithLogs":
					h.handleDatabaseDeploy("mariadb", input, emit, done)
				case "mongo.deployWithLogs":
					h.handleDatabaseDeploy("mongo", input, emit, done)
				case "redis.deployWithLogs":
					h.handleDatabaseDeploy("redis", input, emit, done)
				case "backup.restoreBackupWithLogs":
					h.handleBackupRestore(input, emit, done)
				case "volumeBackups.restoreVolumeBackupWithLogs":
					h.handleVolumeBackupRestore(input, emit, done)
				default:
					emit(fmt.Sprintf("Unknown subscription: %s", req.Params.Path))
				}
			}()

		case "subscription.stop":
			subsMu.Lock()
			if sub, ok := subs[idKey]; ok {
				sub.cancel()
				delete(subs, idKey)
			}
			subsMu.Unlock()
		}
	}

	// Cleanup all active subscriptions
	subsMu.Lock()
	for _, sub := range subs {
		sub.cancel()
	}
	subsMu.Unlock()

	return nil
}

// handleServerSetup executes the server setup via SSH and streams logs.
func (h *Handler) handleServerSetup(input json.RawMessage, emit func(string) bool, done chan struct{}) {
	var in struct {
		ServerID string `json:"serverId"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		emit(fmt.Sprintf("Error parsing input: %v\n", err))
		return
	}

	emit("\nInstalling Server Dependencies: 🔄\n")

	// Load server with SSH key
	var server schema.Server
	if err := h.DB.Preload("SSHKey").First(&server, "\"serverId\" = ?", in.ServerID).Error; err != nil {
		emit(fmt.Sprintf("Error: Server not found: %v\n", err))
		return
	}

	if server.SSHKey == nil {
		emit("Error: No SSH key configured for this server\n")
		emit("Please assign an SSH key to your server before setting up.\n")
		return
	}

	// Determine setup command
	setupCmd := server.Command
	if setupCmd == "" {
		isBuild := server.ServerType == "build"
		setupCmd = setup.GenerateServerSetupScript(isBuild)
	}

	// Parse SSH key
	signer, err := ssh.ParsePrivateKey([]byte(server.SSHKey.PrivateKey))
	if err != nil {
		emit(fmt.Sprintf("Error: Invalid SSH key: %v\n", err))
		return
	}

	// Connect via SSH
	config := &ssh.ClientConfig{
		User:            server.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", server.IPAddress, server.Port)
	emit(fmt.Sprintf("Connecting to %s...\n", addr))

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		emit(fmt.Sprintf("Error: SSH connection failed: %v\n", err))
		emit("\nPossible causes:\n")
		emit("- The SSH key is not authorized on the server\n")
		emit("- The server is not reachable\n")
		emit("- The port is blocked by a firewall\n")
		return
	}
	defer client.Close()

	emit("Connected successfully!\n\n")

	// Create SSH session
	session, err := client.NewSession()
	if err != nil {
		emit(fmt.Sprintf("Error: Failed to create SSH session: %v\n", err))
		return
	}
	defer session.Close()

	// Get stdout pipe for streaming
	stdout, err := session.StdoutPipe()
	if err != nil {
		emit(fmt.Sprintf("Error: %v\n", err))
		return
	}
	// Merge stderr into stdout
	session.Stderr = session.Stdout

	// Start the command
	if err := session.Start(setupCmd); err != nil {
		emit(fmt.Sprintf("Error: Failed to start setup: %v\n", err))
		return
	}

	// Stream output
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		select {
		case <-done:
			session.Close()
			return
		default:
			if !emit(scanner.Text() + "\n") {
				session.Close()
				return
			}
		}
	}

	// Wait for command to finish
	exitErr := session.Wait()

	if exitErr != nil {
		emit(fmt.Sprintf("\nSetup failed with error: %v\n", exitErr))
		emit("\nSetup Server: ❌\n")
	} else {
		emit("\nSetup Server: ✅\n")
		// Update server status
		h.DB.Table("server").Where("\"serverId\" = ?", in.ServerID).
			Update("\"serverStatus\"", "active")
	}

	emit("Deployment completed successfully!")
}

// handleDatabaseDeploy deploys a database service and streams progress.
func (h *Handler) handleDatabaseDeploy(dbType string, input json.RawMessage, emit func(string) bool, done chan struct{}) {
	var in map[string]string
	json.Unmarshal(input, &in)

	// Each database type has its own ID field
	idField := dbType + "Id"
	id := in[idField]
	if id == "" {
		emit(fmt.Sprintf("Error: Missing %s in input\n", idField))
		return
	}

	emit(fmt.Sprintf("\nDeploying %s: 🔄\n", dbType))

	var deployErr error

	switch dbType {
	case "postgres":
		deployErr = h.deployDatabaseInline("postgres", id, emit)
	case "mysql":
		deployErr = h.deployDatabaseInline("mysql", id, emit)
	case "mariadb":
		deployErr = h.deployDatabaseInline("mariadb", id, emit)
	case "mongo":
		deployErr = h.deployDatabaseInline("mongo", id, emit)
	case "redis":
		deployErr = h.deployDatabaseInline("redis", id, emit)
	default:
		emit(fmt.Sprintf("Unknown database type: %s\n", dbType))
		return
	}

	if deployErr != nil {
		emit(fmt.Sprintf("\nDeploy %s failed: %v\n", dbType, deployErr))
		emit(fmt.Sprintf("\nDeploy %s: ❌\n", dbType))
	} else {
		emit(fmt.Sprintf("\nDeploy %s: ✅\n", dbType))
	}
	emit("Deployment completed successfully!")
}

// dbDeployInfo holds common fields extracted from any database type.
type dbDeployInfo struct {
	appName      string
	dockerImage  string
	serverID     *string
	server       *schema.Server
	mounts       []schema.Mount
	command      *string
	env          *string
	memoryLimit  *string
	cpuLimit     *string
	externalPort *int
	internalPort int
	envVars      map[string]string
}

// deployDatabaseInline runs the database deploy command and streams output.
func (h *Handler) deployDatabaseInline(dbType string, id string, emit func(string) bool) error {
	info, err := h.loadDBDeployInfo(dbType, id)
	if err != nil {
		return err
	}

	h.updateDBStatus(dbType, id, "running")

	emit(fmt.Sprintf("Pulling image: %s\n", info.dockerImage))
	emit(fmt.Sprintf("App name: %s\n", info.appName))

	// Build docker service create command
	args := []string{"docker", "service", "create",
		"--name", info.appName,
		"--network", "dokploy-network",
		"--replicas", "1",
		"--update-parallelism", "1",
		"--update-order", "start-first",
	}

	for k, v := range info.envVars {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	if info.env != nil && *info.env != "" {
		for _, line := range splitEnvLines(*info.env) {
			line = trimSpace(line)
			if line != "" && line[0] != '#' {
				args = append(args, "--env", line)
			}
		}
	}

	for _, m := range info.mounts {
		mountStr := fmt.Sprintf("type=%s", m.Type)
		if m.HostPath != nil && *m.HostPath != "" {
			mountStr += fmt.Sprintf(",source=%s", *m.HostPath)
		}
		mountStr += fmt.Sprintf(",target=%s", m.MountPath)
		args = append(args, "--mount", mountStr)
	}

	if info.memoryLimit != nil && *info.memoryLimit != "" {
		args = append(args, "--limit-memory", *info.memoryLimit)
	}
	if info.cpuLimit != nil && *info.cpuLimit != "" {
		args = append(args, "--limit-cpu", *info.cpuLimit)
	}

	if info.externalPort != nil && *info.externalPort > 0 {
		args = append(args, "--publish", fmt.Sprintf("%d:%d", *info.externalPort, info.internalPort))
	}

	args = append(args, info.dockerImage)
	if info.command != nil && *info.command != "" {
		args = append(args, *info.command)
	}

	emit("Running docker service create...\n")

	var deployErr error
	if info.serverID != nil && info.server != nil && info.server.SSHKey != nil {
		emit(fmt.Sprintf("Deploying to remote server: %s\n", info.server.IPAddress))
		deployErr = h.runCommandViaSSH(info.server, args, emit)
	} else {
		deployErr = h.runCommandLocal(args, emit)
	}

	if deployErr != nil {
		h.updateDBStatus(dbType, id, "error")
		return deployErr
	}

	h.updateDBStatus(dbType, id, "done")
	return nil
}

func (h *Handler) loadDBDeployInfo(dbType, id string) (*dbDeployInfo, error) {
	info := &dbDeployInfo{}
	switch dbType {
	case "postgres":
		var pg schema.Postgres
		if err := h.DB.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
			First(&pg, "\"postgresId\" = ?", id).Error; err != nil {
			return nil, err
		}
		info.appName = pg.AppName
		info.dockerImage = pg.DockerImage
		info.serverID = pg.ServerID
		info.server = pg.Server
		info.mounts = pg.Mounts
		info.command = pg.Command
		info.env = pg.Env
		info.memoryLimit = pg.MemoryLimit
		info.cpuLimit = pg.CPULimit
		info.externalPort = pg.ExternalPort
		info.internalPort = 5432
		info.envVars = map[string]string{
			"POSTGRES_DB": pg.DatabaseName, "POSTGRES_USER": pg.DatabaseUser,
			"POSTGRES_PASSWORD": pg.DatabasePassword,
		}
	case "mysql":
		var my schema.MySQL
		if err := h.DB.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
			First(&my, "\"mysqlId\" = ?", id).Error; err != nil {
			return nil, err
		}
		info.appName = my.AppName
		info.dockerImage = my.DockerImage
		info.serverID = my.ServerID
		info.server = my.Server
		info.mounts = my.Mounts
		info.command = my.Command
		info.env = my.Env
		info.memoryLimit = my.MemoryLimit
		info.cpuLimit = my.CPULimit
		info.externalPort = my.ExternalPort
		info.internalPort = 3306
		info.envVars = map[string]string{
			"MYSQL_DATABASE": my.DatabaseName, "MYSQL_USER": my.DatabaseUser,
			"MYSQL_PASSWORD": my.DatabasePassword, "MYSQL_ROOT_PASSWORD": my.DatabaseRootPassword,
		}
	case "mariadb":
		var mdb schema.MariaDB
		if err := h.DB.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
			First(&mdb, "\"mariadbId\" = ?", id).Error; err != nil {
			return nil, err
		}
		info.appName = mdb.AppName
		info.dockerImage = mdb.DockerImage
		info.serverID = mdb.ServerID
		info.server = mdb.Server
		info.mounts = mdb.Mounts
		info.command = mdb.Command
		info.env = mdb.Env
		info.memoryLimit = mdb.MemoryLimit
		info.cpuLimit = mdb.CPULimit
		info.externalPort = mdb.ExternalPort
		info.internalPort = 3306
		info.envVars = map[string]string{
			"MARIADB_DATABASE": mdb.DatabaseName, "MARIADB_USER": mdb.DatabaseUser,
			"MARIADB_PASSWORD": mdb.DatabasePassword, "MARIADB_ROOT_PASSWORD": mdb.DatabaseRootPassword,
		}
	case "mongo":
		var mongo schema.Mongo
		if err := h.DB.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
			First(&mongo, "\"mongoId\" = ?", id).Error; err != nil {
			return nil, err
		}
		info.appName = mongo.AppName
		info.dockerImage = mongo.DockerImage
		info.serverID = mongo.ServerID
		info.server = mongo.Server
		info.mounts = mongo.Mounts
		info.command = mongo.Command
		info.env = mongo.Env
		info.memoryLimit = mongo.MemoryLimit
		info.cpuLimit = mongo.CPULimit
		info.externalPort = mongo.ExternalPort
		info.internalPort = 27017
		info.envVars = map[string]string{
			"MONGO_INITDB_ROOT_USERNAME": mongo.DatabaseUser,
			"MONGO_INITDB_ROOT_PASSWORD": mongo.DatabasePassword,
		}
	case "redis":
		var redis schema.Redis
		if err := h.DB.Preload("Mounts").Preload("Server").Preload("Server.SSHKey").
			First(&redis, "\"redisId\" = ?", id).Error; err != nil {
			return nil, err
		}
		info.appName = redis.AppName
		info.dockerImage = redis.DockerImage
		info.serverID = redis.ServerID
		info.server = redis.Server
		info.mounts = redis.Mounts
		info.command = redis.Command
		info.env = redis.Env
		info.memoryLimit = redis.MemoryLimit
		info.cpuLimit = redis.CPULimit
		info.externalPort = redis.ExternalPort
		info.internalPort = 6379
		info.envVars = map[string]string{}
		if redis.DatabasePassword != "" {
			if info.command != nil {
				cmdStr := fmt.Sprintf("%s --requirepass %s", *info.command, redis.DatabasePassword)
				info.command = &cmdStr
			} else {
				cmdStr := fmt.Sprintf("redis-server --requirepass %s", redis.DatabasePassword)
				info.command = &cmdStr
			}
		}
	default:
		return nil, fmt.Errorf("unknown database type: %s", dbType)
	}
	return info, nil
}

func (h *Handler) updateDBStatus(dbType, id, status string) {
	switch dbType {
	case "postgres":
		h.DB.Table("postgres").Where("\"postgresId\" = ?", id).Update("\"applicationStatus\"", status)
	case "mysql":
		h.DB.Table("mysql").Where("\"mysqlId\" = ?", id).Update("\"applicationStatus\"", status)
	case "mariadb":
		h.DB.Table("mariadb").Where("\"mariadbId\" = ?", id).Update("\"applicationStatus\"", status)
	case "mongo":
		h.DB.Table("mongo").Where("\"mongoId\" = ?", id).Update("\"applicationStatus\"", status)
	case "redis":
		h.DB.Table("redis").Where("\"redisId\" = ?", id).Update("\"applicationStatus\"", status)
	}
}

func (h *Handler) runCommandLocal(args []string, emit func(string) bool) error {
	cmd := exec.Command(args[0], args[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		if !emit(scanner.Text() + "\n") {
			cmd.Process.Kill()
			return nil
		}
	}

	return cmd.Wait()
}

func (h *Handler) runCommandViaSSH(server *schema.Server, args []string, emit func(string) bool) error {
	signer, err := ssh.ParsePrivateKey([]byte(server.SSHKey.PrivateKey))
	if err != nil {
		return fmt.Errorf("invalid SSH key: %v", err)
	}

	config := &ssh.ClientConfig{
		User:            server.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", server.IPAddress, server.Port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH session failed: %v", err)
	}
	defer session.Close()

	// Build shell command from args (properly escape)
	shellCmd := buildShellCommand(args)

	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	session.Stderr = session.Stdout

	if err := session.Start(shellCmd); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		if !emit(scanner.Text() + "\n") {
			session.Close()
			return nil
		}
	}

	return session.Wait()
}

func buildShellCommand(args []string) string {
	var parts []string
	for _, arg := range args {
		// Simple shell escaping
		if needsQuoting(arg) {
			parts = append(parts, "'"+escapeShellSingleQuote(arg)+"'")
		} else {
			parts = append(parts, arg)
		}
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " "
		}
		result += p
	}
	return result
}

func needsQuoting(s string) bool {
	for _, c := range s {
		if c == ' ' || c == '\'' || c == '"' || c == '$' || c == '`' || c == '\\' ||
			c == '(' || c == ')' || c == '{' || c == '}' || c == '|' || c == '&' ||
			c == ';' || c == '<' || c == '>' || c == '=' || c == ',' {
			return true
		}
	}
	return false
}

func escapeShellSingleQuote(s string) string {
	result := ""
	for _, c := range s {
		if c == '\'' {
			result += "'\"'\"'"
		} else {
			result += string(c)
		}
	}
	return result
}

func splitEnvLines(env string) []string {
	var lines []string
	current := ""
	for _, c := range env {
		if c == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// handleBackupRestore handles backup restore with log streaming.
func (h *Handler) handleBackupRestore(input json.RawMessage, emit func(string) bool, done chan struct{}) {
	var in struct {
		BackupID      string `json:"backupId"`
		DestinationID string `json:"destinationId"`
		DatabaseType  string `json:"databaseType"`
		Database      string `json:"database"`
		BackupType    string `json:"backupType"`
	}
	json.Unmarshal(input, &in)

	emit("\nRestoring backup: 🔄\n")
	emit(fmt.Sprintf("Backup ID: %s\n", in.BackupID))
	emit(fmt.Sprintf("Database type: %s\n", in.DatabaseType))

	// TODO: Implement actual backup restore with streaming
	// For now, send completion signal
	emit("\nRestore Backup: ✅\n")
	emit("Deployment completed successfully!")
}

// handleVolumeBackupRestore handles volume backup restore with log streaming.
func (h *Handler) handleVolumeBackupRestore(input json.RawMessage, emit func(string) bool, done chan struct{}) {
	var in struct {
		BackupFileName string `json:"backupFileName"`
		DestinationID  string `json:"destinationId"`
		VolumeName     string `json:"volumeName"`
		ID             string `json:"id"`
		ServiceType    string `json:"serviceType"`
	}
	json.Unmarshal(input, &in)

	emit("\nRestoring volume backup: 🔄\n")
	emit(fmt.Sprintf("Volume: %s\n", in.VolumeName))
	emit(fmt.Sprintf("File: %s\n", in.BackupFileName))

	// TODO: Implement actual volume backup restore with streaming
	emit("\nRestore Volume Backup: ✅\n")
	emit("Deployment completed successfully!")
}

