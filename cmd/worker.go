package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/queuectl/queuectl/core"
	"github.com/spf13/cobra"
)

var (
	workerCount int
	detach      bool
	foreground  bool
	workerIDArg string
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Manage queuectl workers",
}

var workerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start worker processes",
	Run: func(cmd *cobra.Command, args []string) {
		if detach {
			startDetachedWorkers()
		} else {
			startForegroundWorkers()
		}
	},
}

var workerRunCmd = &cobra.Command{
	Use:    "run",
	Hidden: true, // internal command used by --detach
	Run: func(cmd *cobra.Command, args []string) {
		q, err := initQueue()
		failErr(err)

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		var wg sync.WaitGroup
		wg.Add(1)

		w := core.NewWorker(workerIDArg, q)

		// Run lease sweeper in this worker as well
		sweeper := core.NewLeaseSweeper(q)
		go sweeper.Run(ctx)

		go w.Start(ctx, &wg)

		<-ctx.Done()
		fmt.Println("Worker run received shutdown signal, finishing in-flight jobs...")
		wg.Wait()
		fmt.Println("Worker run stopped cleanly.")
	},
}

var workerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop detached worker processes gracefully",
	Run: func(cmd *cobra.Command, args []string) {
		home, _ := os.UserHomeDir()
		pidFile := filepath.Join(home, ".queuectl", "workers.pid")

		data, err := os.ReadFile(pidFile)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No active workers found.")
				return
			}
			failErr(err)
		}

		pids := strings.Split(strings.TrimSpace(string(data)), "\n")
		var validPids []int
		for _, p := range pids {
			if pid, err := strconv.Atoi(p); err == nil {
				validPids = append(validPids, pid)
			}
		}

		if len(validPids) == 0 {
			fmt.Println("No valid PIDs found in pidfile.")
			return
		}

		fmt.Printf("Stopping %d workers...\n", len(validPids))
		for _, pid := range validPids {
			process, err := os.FindProcess(pid)
			if err == nil {
				if err := process.Signal(syscall.SIGTERM); err != nil {
					process.Kill()
				}
			}
		}

		// Wait loop (simple version)
		timeout := time.After(30 * time.Second)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-timeout:
				fmt.Println("Timeout reached waiting for workers to stop.")
				return
			case <-ticker.C:
				allDead := true
				for _, pid := range validPids {
					if isProcessAlive(pid) {
						allDead = false
						break
					}
				}
				if allDead {
					os.Remove(pidFile)
					fmt.Println("All workers stopped.")
					return
				}
			}
		}
	},
}

func init() {
	workerStartCmd.Flags().IntVar(&workerCount, "count", 1, "number of workers")
	workerStartCmd.Flags().BoolVar(&detach, "detach", false, "run workers as OS subprocesses")
	workerStartCmd.Flags().BoolVar(&foreground, "foreground", true, "run workers in foreground")

	workerRunCmd.Flags().StringVar(&workerIDArg, "id", "", "worker ID")

	workerCmd.AddCommand(workerStartCmd)
	workerCmd.AddCommand(workerStopCmd)
	workerCmd.AddCommand(workerRunCmd)

	rootCmd.AddCommand(workerCmd)
}

func startForegroundWorkers() {
	q, err := initQueue()
	failErr(err)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	// Start lease sweeper
	sweeper := core.NewLeaseSweeper(q)
	go sweeper.Run(ctx)

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		w := core.NewWorker(fmt.Sprintf("fg-%d", i), q)
		go w.Start(ctx, &wg)
	}

	fmt.Printf("Started %d foreground workers. Press Ctrl+C to stop.\n", workerCount)
	<-ctx.Done()
	fmt.Println("\nShutdown signal received, finishing in-flight jobs...")

	// Start a hard kill timer
	go func() {
		<-time.After(30 * time.Second)
		fmt.Println("Shutdown timeout reached. Forcing exit.")
		os.Exit(1)
	}()

	wg.Wait()
	fmt.Println("All workers stopped cleanly.")
}

func startDetachedWorkers() {
	home, _ := os.UserHomeDir()
	pidFile := filepath.Join(home, ".queuectl", "workers.pid")

	var pids []string

	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}

	for i := 0; i < workerCount; i++ {
		cmd := exec.Command(exe, "worker", "run", "--id", fmt.Sprintf("dt-%d-%d", os.Getpid(), i))
		if dbPath != "" {
			cmd.Args = append(cmd.Args, "--db-path", dbPath)
		}
		if cfgFile != "" {
			cmd.Args = append(cmd.Args, "--config", cfgFile)
		}

		if err := cmd.Start(); err != nil {
			failErr(err)
		}

		pids = append(pids, strconv.Itoa(cmd.Process.Pid))
	}

	// Ensure directory exists
	os.MkdirAll(filepath.Dir(pidFile), 0755)

	// Append to pid file
	f, err := os.OpenFile(pidFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	failErr(err)
	defer f.Close()

	for _, pid := range pids {
		f.WriteString(pid + "\n")
	}

	fmt.Printf("Started %d detached workers. PIDs saved to %s\n", workerCount, pidFile)
}
