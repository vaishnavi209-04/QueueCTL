package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/queuectl/queuectl/internal/model"
	"github.com/spf13/cobra"
)

var dlqRetryAll bool

var dlqCmd = &cobra.Command{
	Use:   "dlq",
	Short: "Manage the Dead Letter Queue (jobs with state 'dead')",
}

var dlqListCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs in the DLQ",
	Run: func(cmd *cobra.Command, args []string) {
		q, err := initQueue()
		failErr(err)

		s := model.StateDead
		jobs, err := q.Store().ListJobs(&s, listLimit)
		failErr(err)

		if jsonOut {
			printJSON(jobs)
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tCOMMAND\tUPDATED_AT\tERROR")

		for _, j := range jobs {
			cmdStr := j.Command
			if len(cmdStr) > 20 {
				cmdStr = cmdStr[:17] + "..."
			}
			errStr := ""
			if j.LastError != nil {
				errStr = *j.LastError
				if len(errStr) > 40 {
					errStr = strings.ReplaceAll(errStr[:37]+"...", "\n", " ")
				} else {
					errStr = strings.ReplaceAll(errStr, "\n", " ")
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				j.ID, cmdStr, j.UpdatedAt.Format("2006-01-02 15:04:05"), errStr)
		}
		w.Flush()
	},
}

var dlqRetryCmd = &cobra.Command{
	Use:   "retry [job_id]",
	Short: "Retry a dead job (moves back to pending)",
	Run: func(cmd *cobra.Command, args []string) {
		q, err := initQueue()
		failErr(err)

		if dlqRetryAll {
			count, err := q.Store().RetryAllDead()
			failErr(err)
			fmt.Printf("Retried %d dead jobs\n", count)
			return
		}

		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "error: requires a job id or --all flag")
			os.Exit(1)
		}

		jobID := args[0]

		// Guardrail: Ensure it is dead first
		job, err := q.Store().GetJob(jobID)
		failErr(err)

		if job.State != model.StateDead {
			fmt.Fprintf(os.Stderr, "error: job %s is not dead (current state: %s)\n", jobID, job.State)
			os.Exit(1)
		}

		err = q.Store().RetryDead(jobID)
		failErr(err)

		fmt.Printf("Job %s state reset to pending (attempts reset)\n", jobID)
	},
}

func init() {
	dlqListCmd.Flags().IntVar(&listLimit, "limit", 20, "limit number of results")

	dlqRetryCmd.Flags().BoolVar(&dlqRetryAll, "all", false, "retry all dead jobs")

	dlqCmd.AddCommand(dlqListCmd)
	dlqCmd.AddCommand(dlqRetryCmd)

	rootCmd.AddCommand(dlqCmd)
}
