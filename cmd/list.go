package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/queuectl/queuectl/internal/model"
	"github.com/spf13/cobra"
)

var (
	listState string
	listLimit int
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs",
	Run: func(cmd *cobra.Command, args []string) {
		q, err := initQueue()
		failErr(err)

		var stateFilter *model.JobState
		if listState != "" {
			s := model.JobState(listState)
			stateFilter = &s
		}

		jobs, err := q.Store().ListJobs(stateFilter, listLimit)
		failErr(err)

		if jsonOut {
			printJSON(jobs)
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATE\tATTEMPTS\tCOMMAND\tUPDATED_AT")

		for _, j := range jobs {
			cmdStr := j.Command
			if len(cmdStr) > 30 {
				cmdStr = cmdStr[:27] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%d/%d\t%s\t%s\n",
				j.ID, j.State, j.Attempts, j.MaxRetries, cmdStr, j.UpdatedAt.Format("2006-01-02 15:04:05"))
		}
		w.Flush()
	},
}

func init() {
	listCmd.Flags().StringVar(&listState, "state", "", "filter by state")
	listCmd.Flags().IntVar(&listLimit, "limit", 20, "limit number of results")
	rootCmd.AddCommand(listCmd)
}
