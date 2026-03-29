// Input: gorilla/websocket, docker SDK, tRPC WS 订阅协议
// Output: DrawerLogs WebSocket 端点 (tRPC subscription 兼容的实时日志流)
// Role: tRPC 订阅兼容的 WebSocket 端点，为前端 Drawer 组件提供实时 Docker 服务日志
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package ws

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
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

// handleDatabaseDeploy 部署数据库服务（与 TS 版对齐：直接调用 service 层，不走队列）
func (h *Handler) handleDatabaseDeploy(dbType string, input json.RawMessage, emit func(string) bool, done chan struct{}) {
	var in map[string]string
	json.Unmarshal(input, &in)

	idField := dbType + "Id"
	id := in[idField]
	if id == "" {
		emit(fmt.Sprintf("Error: Missing %s in input\n", idField))
		return
	}

	if h.DBSvc == nil {
		emit("Error: Database service not available\n")
		return
	}

	// 与 TS 版对齐：直接调用 service 层的部署函数，通过回调实时输出日志
	onData := func(msg string) {
		emit(msg)
	}

	if err := h.DBSvc.DeployByType(id, dbType, onData); err != nil {
		emit(fmt.Sprintf("\nDeploy %s: ❌\n", dbType))
	} else {
		emit(fmt.Sprintf("\nDeploy %s: ✅\n", dbType))
	}
}



// handleBackupRestore 处理备份恢复，调用真正的 restore 逻辑并流式输出日志。
// 输入字段与前端 restoreBackupWithLogs subscription 一致。
func (h *Handler) handleBackupRestore(input json.RawMessage, emit func(string) bool, done chan struct{}) {
	var in struct {
		DatabaseID    string          `json:"databaseId"`
		DatabaseType  string          `json:"databaseType"`
		BackupType    string          `json:"backupType"`
		DatabaseName  string          `json:"databaseName"`
		BackupFile    string          `json:"backupFile"`
		DestinationID string          `json:"destinationId"`
		Metadata      json.RawMessage `json:"metadata"`
	}
	json.Unmarshal(input, &in)

	emit(fmt.Sprintf("\nRestoring backup: 🔄\n"))
	emit(fmt.Sprintf("Database type: %s\n", in.DatabaseType))
	emit(fmt.Sprintf("Backup file: %s\n", in.BackupFile))

	if h.BackupSvc == nil {
		emit("Error: backup service not available\n")
		return
	}

	emitFn := func(log string) { emit(log + "\n") }

	if in.BackupType == "compose" {
		// Compose 恢复
		metadataStr := ""
		if in.Metadata != nil {
			metadataStr = string(in.Metadata)
		}
		if err := h.BackupSvc.RestoreComposeBackup(in.DatabaseID, in.DestinationID, in.DatabaseType, in.DatabaseName, in.BackupFile, metadataStr, emitFn); err != nil {
			emit(fmt.Sprintf("Error: %s\n", err.Error()))
			return
		}
	} else if in.DatabaseType == "web-server" {
		// Web Server 恢复
		if err := h.BackupSvc.RestoreWebServerBackup(in.DestinationID, in.BackupFile, emitFn); err != nil {
			emit(fmt.Sprintf("Error: %s\n", err.Error()))
			return
		}
	} else {
		// 数据库恢复（postgres/mysql/mariadb/mongo）
		if err := h.BackupSvc.RestoreBackup(in.DatabaseID, in.DestinationID, in.DatabaseType, in.DatabaseName, in.BackupFile, emitFn); err != nil {
			emit(fmt.Sprintf("Error: %s\n", err.Error()))
			return
		}
	}

	emit("\nRestore Backup: ✅\n")
	emit("Restore completed successfully!")
}

// handleVolumeBackupRestore 处理卷备份恢复。
// 与 TS 版一致：直接接收所有参数（destinationId, volumeName, backupFileName, id, serviceType, serverId），
// 不查 VolumeBackup 记录。
func (h *Handler) handleVolumeBackupRestore(input json.RawMessage, emit func(string) bool, done chan struct{}) {
	var in struct {
		ID             string `json:"id"`
		ServiceType    string `json:"serviceType"`
		ServerID       string `json:"serverId"`
		DestinationID  string `json:"destinationId"`
		VolumeName     string `json:"volumeName"`
		BackupFileName string `json:"backupFileName"`
	}
	json.Unmarshal(input, &in)

	emit(fmt.Sprintf("\nRestoring volume backup: 🔄\n"))
	emit(fmt.Sprintf("Volume: %s\n", in.VolumeName))
	emit(fmt.Sprintf("File: %s\n", in.BackupFileName))

	if h.BackupSvc == nil {
		emit("Error: backup service not available\n")
		return
	}

	// BackupSvc 需要实现 RestoreVolumeBackup 接口
	type volumeRestorer interface {
		RestoreVolumeBackup(destinationID, volumeName, backupFileName, serviceID, serviceType, serverID string, onData func(string)) error
	}
	if vr, ok := h.BackupSvc.(volumeRestorer); ok {
		onData := func(data string) { emit(data + "\n") }
		if err := vr.RestoreVolumeBackup(in.DestinationID, in.VolumeName, in.BackupFileName, in.ID, in.ServiceType, in.ServerID, onData); err != nil {
			emit(fmt.Sprintf("Error: %s\n", err.Error()))
			return
		}
	} else {
		emit("Error: volume backup restore not supported\n")
		return
	}

	emit("\nRestore Volume Backup: ✅\n")
	emit("Restore completed successfully!")
}

