package process

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// ExecError represents a command execution error with additional context.
type ExecError struct {
	Message  string
	Command  string
	Stdout   string
	Stderr   string
	ExitCode int
	ServerID string
	Err      error
}

func (e *ExecError) Error() string {
	return e.Message
}

func (e *ExecError) Unwrap() error {
	return e.Err
}

// ExecResult holds the result of a command execution.
type ExecResult struct {
	Stdout string
	Stderr string
}

// ExecAsync runs a command and returns the combined stdout/stderr.
func ExecAsync(command string, opts ...ExecOption) (*ExecResult, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	ctx := context.Background()
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if o.Dir != "" {
		cmd.Dir = o.Dir
	}
	if o.Env != nil {
		cmd.Env = o.Env
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return nil, &ExecError{
			Message:  fmt.Sprintf("Command execution failed: %s", err.Error()),
			Command:  command,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: exitCode,
			Err:      err,
		}
	}

	return &ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}, nil
}

// ExecAsyncStream runs a command and streams output to the onData callback.
func ExecAsyncStream(command string, onData func(string), opts ...ExecOption) (*ExecResult, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	cmd := exec.Command("sh", "-c", command)
	if o.Dir != "" {
		cmd.Dir = o.Dir
	}
	if o.Env != nil {
		cmd.Env = o.Env
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var stdoutBuf, stderrBuf strings.Builder

	// Read stdout
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdoutPipe.Read(buf)
			if n > 0 {
				data := string(buf[:n])
				stdoutBuf.WriteString(data)
				if onData != nil {
					onData(data)
				}
			}
			if err == io.EOF || err != nil {
				break
			}
		}
	}()

	// Read stderr
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				data := string(buf[:n])
				stderrBuf.WriteString(data)
				if onData != nil {
					onData(data)
				}
			}
			if err == io.EOF || err != nil {
				break
			}
		}
	}()

	err = cmd.Wait()
	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return nil, &ExecError{
			Message:  fmt.Sprintf("Command execution failed: %s", err.Error()),
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

// ExecOption configures command execution.
type ExecOption func(*execOptions)

type execOptions struct {
	Dir     string
	Env     []string
	Timeout time.Duration
}

func defaultOptions() *execOptions {
	return &execOptions{}
}

// WithDir sets the working directory.
func WithDir(dir string) ExecOption {
	return func(o *execOptions) { o.Dir = dir }
}

// WithEnv sets environment variables.
func WithEnv(env []string) ExecOption {
	return func(o *execOptions) { o.Env = env }
}

// WithTimeout sets the execution timeout.
func WithTimeout(d time.Duration) ExecOption {
	return func(o *execOptions) { o.Timeout = d }
}

// Sleep pauses for the given duration.
func Sleep(d time.Duration) {
	time.Sleep(d)
}
