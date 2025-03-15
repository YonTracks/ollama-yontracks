package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"log/slog"

	"github.com/ollama/ollama/api"
)

// restartChan signals that the server should restart.
var restartChan = make(chan struct{}, 1)

// RequestServerRestart triggers an immediate server restart.
func RequestServerRestart() {
	slog.Info("Server restart triggered.")
	select {
	case restartChan <- struct{}{}:
		slog.Info("Server restart signal sent successfully.")
	default:
		slog.Warn("Restart already in progress.")
	}
}

// getCLIFullPath attempts to resolve the full path for the given command.
func getCLIFullPath(command string) string {
	// checkPaths is a helper to return path if it exists.
	checkPaths := func(paths ...string) (string, bool) {
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				return p, true
			}
		}
		return "", false
	}

	appExe, err := os.Executable()
	if err == nil {
		if p, ok := checkPaths(
			filepath.Join(filepath.Dir(appExe), command),
			filepath.Join(filepath.Dir(appExe), "bin", command),
		); ok {
			return p
		}
	}

	if p, err := exec.LookPath(command); err == nil {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	if pwd, err := os.Getwd(); err == nil {
		if p, ok := checkPaths(filepath.Join(pwd, command)); ok {
			return p
		}
	}

	return command
}

// backoffDelay returns a delay based on the crash count with an upper limit.
func backoffDelay(crashCount int) time.Duration {
	delay := time.Duration(crashCount) * 100 * time.Millisecond
	if delay > 5*time.Second {
		return 5 * time.Second
	}
	return delay
}

func start(ctx context.Context, command string) (*exec.Cmd, error) {
	cmd := getCmd(ctx, getCLIFullPath(command))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to spawn server stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to spawn server stderr pipe: %w", err)
	}

	rotateLogs(ServerLogFile)
	logFile, err := os.OpenFile(ServerLogFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o755)
	if err != nil {
		return nil, fmt.Errorf("failed to create server log: %w", err)
	}

	logDir := filepath.Dir(ServerLogFile)
	_, err = os.Stat(logDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat ollama server log dir %s: %v", logDir, err)
		}

		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return nil, fmt.Errorf("create ollama server log dir %s: %v", logDir, err)
		}
	}

	go func() {
		defer logFile.Close()
		io.Copy(logFile, stdout) //nolint:errcheck
	}()
	go func() {
		defer logFile.Close()
		io.Copy(logFile, stderr) //nolint:errcheck
	}()

	// Re-wire context done behavior to attempt a graceful shutdown of the server
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			err := terminate(cmd)
			if err != nil {
				slog.Warn("error trying to gracefully terminate server", "err", err)
				return cmd.Process.Kill()
			}

			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()

			for {
				select {
				case <-tick.C:
					exited, err := isProcessExited(cmd.Process.Pid)
					if err != nil {
						return err
					}

					if exited {
						return nil
					}
				case <-time.After(5 * time.Second):
					slog.Warn("graceful server shutdown timeout, killing", "pid", cmd.Process.Pid)
					return cmd.Process.Kill()
				}
			}
		}
		return nil
	}

	// run the command and wait for it to finish
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start server %w", err)
	}
	if cmd.Process != nil {
		slog.Info(fmt.Sprintf("started ollama server with pid %d", cmd.Process.Pid))
	}
	slog.Info(fmt.Sprintf("ollama server logs %s", ServerLogFile))

	return cmd, nil
}

// SpawnServer launches the server process and respawns it on crash/restart.
// worse case scenario error hanling only basic error handling is implemented. TODO: More sophisticated error handling needed.
func SpawnServer(ctx context.Context, command string) (chan int, error) {
	done := make(chan int)
	go func() {
		// Keep the server running unless we're shuttind down the app
		crashCount := 0
		for {
			slog.Info("starting server...")
			cmd, err := start(ctx, command)
			if err != nil {
				crashCount++
				slog.Error(fmt.Sprintf("failed to start server %s", err))
				time.Sleep(500 * time.Millisecond * time.Duration(crashCount))
				continue
			}

			cmd.Wait() //nolint:errcheck
			var code int
			if cmd.ProcessState != nil {
				code = cmd.ProcessState.ExitCode()
			}

			select {
			case <-ctx.Done():
				slog.Info(fmt.Sprintf("server shutdown with exit code %d", code))
				done <- code
				return
			case <-restartChan:
				slog.Info("Test Restart signal received, simulating canceling running server", "pid", cmd.Process.Pid)
			default:
				// Handle critical error: exit code 3221225477.
				if code == 3221225477 {
					slog.Warn("Server crashed. Resetting environment and retrying...", "traking pid", cmd.Process.Pid)
					ResetEnvironmentToDefault()
				}
				if code == 3221226505 {
					slog.Warn("Server crashed - critical driver error detected. ToDO: handle this case")
				}
				crashCount++
				slog.Warn("Server crashed - respawning", "crashCount", crashCount, "exitCode", code)
				// Delay before respawn to allow cleanup.
				time.Sleep(backoffDelay(crashCount) + 200*time.Millisecond)
			}
		}
	}()
	return done, nil
}

// ResetEnvironmentToDefault resets certain environment variables to default settings.
func ResetEnvironmentToDefault() {
	slog.Info("Resetting environment to default settings")
	os.Setenv("CUDA_VISIBLE_DEVICES", "0")
}

// IsServerRunning checks the server's heartbeat via an API client.
func IsServerRunning(ctx context.Context) bool {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		slog.Info("Unable to create API client for server")
		return false
	}
	if err = client.Heartbeat(ctx); err != nil {
		slog.Debug("Server heartbeat error", "error", err)
		slog.Info("Server is not reachable")
		return false
	}
	return true
}
