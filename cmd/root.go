package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/queuectl/queuectl/core"
	"github.com/queuectl/queuectl/store"
)

var (
	dbPath  string
	cfgFile string
	jsonOut bool
)

var rootCmd = &cobra.Command{
	Use:   "queuectl",
	Short: "QueueCTL is a single-binary, embedded-storage, in-process worker pool",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	home, _ := os.UserHomeDir()
	defaultDbPath := filepath.Join(home, ".queuectl", "queue.db")
	defaultCfgFile := filepath.Join(home, ".queuectl", "config.yaml")

	rootCmd.PersistentFlags().StringVar(&dbPath, "db-path", defaultDbPath, "path to sqlite database")
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", defaultCfgFile, "path to config file")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output in JSON format")
}

func initQueue() (*core.Queue, error) {
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, err
	}

	if err := s.Init(); err != nil {
		return nil, err
	}

	dbCfg, err := s.GetAllConfig()
	if err != nil {
		return nil, err
	}

	cfg, err := core.LoadConfig(cfgFile, dbCfg)
	if err != nil {
		return nil, err
	}

	return core.NewQueue(s, cfg), nil
}

func printJSON(data interface{}) {
	b, err := json.Marshal(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(b))
}

func failErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func isProcessAlive(pid int) bool {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH")
		out, err := cmd.Output()
		if err != nil {
			return false
		}
		return !strings.Contains(string(out), "No tasks are running")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
