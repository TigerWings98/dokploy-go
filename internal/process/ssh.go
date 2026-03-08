// Input: golang.org/x/crypto/ssh, SSHConnection 配置 (host/port/username/privateKey)
// Output: SSHExec (远程命令执行), SSHExecWithCallback (远程执行+实时日志回调)
// Role: 远程 SSH 命令执行引擎，为远程服务器操作提供 SSH 通道执行和日志流转
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package process

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHConnection holds the parameters needed to connect via SSH.
type SSHConnection struct {
	Host       string
	Port       int
	Username   string
	PrivateKey string
}

// ExecAsyncRemote executes a command on a remote server via SSH.
func ExecAsyncRemote(conn SSHConnection, command string, onData func(string)) (*ExecResult, error) {
	signer, err := ssh.ParsePrivateKey([]byte(conn.PrivateKey))
	if err != nil {
		msg := fmt.Sprintf("Authentication failed: Invalid SSH private key. Error: %s", err.Error())
		if onData != nil {
			onData(friendlySSHKeyError(err))
		}
		return nil, &ExecError{
			Message: msg,
			Command: command,
			Err:     err,
		}
	}

	config := &ssh.ClientConfig{
		User: conn.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         99999 * time.Millisecond,
	}

	addr := fmt.Sprintf("%s:%d", conn.Host, conn.Port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		if onData != nil {
			onData(fmt.Sprintf("SSH connection error: %s", err.Error()))
		}
		return nil, &ExecError{
			Message: fmt.Sprintf("SSH connection error: %s", err.Error()),
			Command: command,
			Err:     err,
		}
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, &ExecError{
			Message: fmt.Sprintf("Failed to create SSH session: %s", err.Error()),
			Command: command,
			Err:     err,
		}
	}
	defer session.Close()

	// Set up pipes for streaming
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := session.Start(command); err != nil {
		if onData != nil {
			onData(err.Error())
		}
		return nil, &ExecError{
			Message: fmt.Sprintf("Remote command execution failed: %s", err.Error()),
			Command: command,
			Err:     err,
		}
	}

	var stdoutBuf, stderrBuf strings.Builder

	// Read stdout
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := stdoutPipe.Read(buf)
			if n > 0 {
				data := string(buf[:n])
				stdoutBuf.WriteString(data)
				if onData != nil {
					onData(data)
				}
			}
			if readErr != nil {
				break
			}
		}
	}()

	// Read stderr
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := stderrPipe.Read(buf)
			if n > 0 {
				data := string(buf[:n])
				stderrBuf.WriteString(data)
				if onData != nil {
					onData(data)
				}
			}
			if readErr != nil {
				break
			}
		}
	}()

	err = session.Wait()
	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		}
		return nil, &ExecError{
			Message:  fmt.Sprintf("Remote command failed with exit code %d", exitCode),
			Command:  command,
			Stdout:   stdoutBuf.String(),
			Stderr:   stderrBuf.String(),
			ExitCode: exitCode,
			Err:      err,
		}
	}

	return &ExecResult{
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
	}, nil
}

func friendlySSHKeyError(err error) string {
	return strings.Join([]string{
		"",
		"Couldn't connect to your server — the SSH key was not accepted.",
		"",
		"This usually means the key doesn't match what's on the server, or the key format is invalid.",
		"",
		fmt.Sprintf("Technical details: %s", err.Error()),
		"",
		"Hints:",
		"  - Check that the SSH key you added in Dokploy is the same one installed on the server (e.g. in ~/.ssh/authorized_keys).",
		"  - Try generating a new SSH key in Dokploy and add only the public key to the server, then try again.",
	}, "\n")
}
