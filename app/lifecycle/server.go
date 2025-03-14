package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"log/slog"

	"github.com/ollama/ollama/api"
	"golang.org/x/sys/windows"
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
	delay := time.Duration(crashCount) * 500 * time.Millisecond
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
	// Ensure log directory exists before opening file.
	logDir := filepath.Dir(ServerLogFile)
	if _, err := os.Stat(logDir); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat server log dir %s: %v", logDir, err)
		}
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return nil, fmt.Errorf("create server log dir %s: %v", logDir, err)
		}
	}

	logFile, err := os.OpenFile(ServerLogFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o755)
	if err != nil {
		return nil, fmt.Errorf("failed to create server log: %w", err)
	}

	// Use a WaitGroup so that we close the log file once both stdout and stderr streams finish.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(logFile, stdout) //nolint:errcheck
	}()
	go func() {
		defer wg.Done()
		io.Copy(logFile, stderr) //nolint:errcheck
	}()
	go func() {
		wg.Wait()
		_ = logFile.Close()
	}()

	// Re-wire context cancellation for graceful shutdown.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// Attempt graceful termination.
			if err := terminate(cmd); err != nil {
				// For specific Windows parameter errors, fall back to force kill.
				if strings.Contains(err.Error(), "parameter") {
					return cmd.Process.Kill()
				}
				slog.Warn("Error during graceful shutdown, killing server", "error", err)
				return cmd.Process.Kill()
			}

			// Use a context with timeout for waiting on graceful shutdown.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					exited, err := isProcessExited(cmd.Process.Pid)
					if err != nil {
						return err
					}
					if exited {
						return nil
					}
				case <-shutdownCtx.Done():
					slog.Warn("Graceful shutdown timed out, killing server", "pid", cmd.Process.Pid)
					return cmd.Process.Kill()
				}
			}
		}
		return nil
	}

	// Start the command.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start server: %w", err)
	}
	if cmd.Process != nil {
		slog.Info("Started ollama server", "pid", cmd.Process.Pid)
		// On Windows, assign the process to a job object to prevent orphaning.
		if runtime.GOOS == "windows" {
			if err := assignProcessToJob(cmd.Process); err != nil {
				slog.Warn("Failed to assign process to job object", "error", err)
			}
		}
	}
	slog.Info("Ollama server logs", "log_file", ServerLogFile)

	return cmd, nil
}

// assignProcessToJob creates a job object (with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE) and assigns the given process to it.
func assignProcessToJob(p *os.Process) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	size := uint32(unsafe.Sizeof(info))
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), size); err != nil {
		windows.CloseHandle(job)
		return err
	}

	hProcess, err := windows.OpenProcess(windows.PROCESS_ALL_ACCESS, false, uint32(p.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return err
	}
	defer windows.CloseHandle(hProcess)

	if err := windows.AssignProcessToJobObject(job, hProcess); err != nil {
		windows.CloseHandle(job)
		return err
	}

	// Keep the job object alive until the process exits.
	go func() {
		for {
			exited, _ := isProcessExited(p.Pid)
			if exited {
				// Delay briefly to ensure all handles are released.
				time.Sleep(200 * time.Millisecond)
				windows.CloseHandle(job)
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return nil
}

// SpawnServer launches the server process and respawns it on crash/restart.
func SpawnServer(ctx context.Context, command string) (chan int, error) {
	done := make(chan int)
	go func() {
		crashCount := 0
		for {
			// Check for cancellation or restart signal before starting.
			select {
			case <-ctx.Done():
				slog.Info("Context canceled, exiting spawn loop")
				done <- 0
				return
			case <-restartChan:
				slog.Info("Restart requested before starting new server; proceeding with restart.")
			default:
			}

			slog.Info("Starting server...")
			cmd, err := start(ctx, command)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					slog.Info("Context canceled, not starting server")
					done <- 0
					return
				}
				crashCount++
				slog.Error("Failed to start server", "error", err)
				time.Sleep(backoffDelay(crashCount))
				continue
			}
			// Reset crash count on successful start.
			crashCount = 0

			// Cancel running process if context is canceled.
			go func(c *exec.Cmd) {
				<-ctx.Done()
				slog.Info("Context canceled, shutting down running server")
				c.Cancel()
			}(cmd)

			// Wait for process exit or restart signal.
			doneChan := make(chan struct{})
			go func() {
				_ = cmd.Wait() //nolint:errcheck
				close(doneChan)
			}()

			select {
			case <-ctx.Done():
				slog.Info("Context canceled, exiting server loop")
				done <- 0
				return
			case <-restartChan:
				slog.Info("Restart signal received, canceling running server", "pid", cmd.Process.Pid)
				cmd.Cancel()
				<-doneChan
			case <-doneChan:
				// Process finished normally.
			}

			var code int
			if cmd.ProcessState != nil {
				code = cmd.ProcessState.ExitCode()
			}

			// Handle critical error: exit code 3221225477.
			if code == 3221225477 {
				slog.Warn("Server crash - critical driver error detected. Resetting environment and retrying...")
				ResetEnvironmentToDefault()
				cmd.Cancel()
				cmd, err = start(ctx, command)
				if err != nil {
					slog.Error("Retry failed to start server", "error", err)
					done <- code
					return
				}
				_ = cmd.Wait() //nolint:errcheck
				if cmd.ProcessState != nil {
					code = cmd.ProcessState.ExitCode()
				}
			}

			// Exit loop on further critical errors.
			if code == 3221225477 || code == 3221226505 {
				slog.Error("Critical error encountered, exiting server loop", "exitCode", code)
				done <- code
				return
			}

			crashCount++
			slog.Warn("Server crashed - respawning", "crashCount", crashCount, "exitCode", code)
			// Delay before respawn to allow cleanup.
			time.Sleep(backoffDelay(crashCount) + 200*time.Millisecond)
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
