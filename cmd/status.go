package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show queue status",
	Run: func(cmd *cobra.Command, args []string) {
		q, err := initQueue()
		failErr(err)

		stats, err := q.Store().GetStats()
		failErr(err)

		// Count active workers from PID file
		activeWorkers := 0
		var activePids []string

		home, _ := os.UserHomeDir()
		pidFile := filepath.Join(home, ".queuectl", "workers.pid")

		if data, err := os.ReadFile(pidFile); err == nil {
			pids := strings.Split(strings.TrimSpace(string(data)), "\n")
			for _, p := range pids {
				if pid, err := strconv.Atoi(p); err == nil {
					if isProcessAlive(pid) {
						activeWorkers++
						activePids = append(activePids, p)
					}
				}
			}
		}

		if jsonOut {
			out := map[string]interface{}{
				"jobs": stats,
				"workers": map[string]interface{}{
					"active": activeWorkers,
					"pids":   activePids,
				},
			}
			printJSON(out)
			return
		}

		fmt.Println("Jobs:")
		fmt.Printf("  pending:    %d\n", stats["pending"])
		fmt.Printf("  processing: %d\n", stats["processing"])
		fmt.Printf("  completed:  %d\n", stats["completed"])
		fmt.Printf("  failed:     %d\n", stats["failed"])
		fmt.Printf("  dead:       %d\n", stats["dead"])
		fmt.Println("Workers:")
		if activeWorkers > 0 {
			fmt.Printf("  active: %d   (pids: %s)\n", activeWorkers, strings.Join(activePids, ", "))
		} else {
			fmt.Println("  active: 0")
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
