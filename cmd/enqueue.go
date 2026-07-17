package cmd

import (
	"fmt"

	"github.com/queuectl/queuectl/internal/model"
	"github.com/spf13/cobra"
)

var enqueueCmd = &cobra.Command{
	Use:   "enqueue '<json>'",
	Short: "Enqueue a new job",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		q, err := initQueue()
		failErr(err)

		req, err := model.ParseEnqueueRequest(args[0], q.Config().MaxRetries)
		failErr(err)

		job, err := q.Enqueue(req)
		failErr(err)

		if jsonOut {
			printJSON(job)
		} else {
			fmt.Printf("enqueued job %s (state=%s)\n", job.ID, job.State)
		}
	},
}

func init() {
	rootCmd.AddCommand(enqueueCmd)
}
