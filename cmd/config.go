package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"strings"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage queue configuration",
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		key := strings.ReplaceAll(args[0], "-", "_")
		val := args[1]

		q, err := initQueue()
		failErr(err)

		err = q.Store().SetConfig(key, val)
		failErr(err)

		fmt.Printf("config %s set to %s\n", key, val)
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration value",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := strings.ReplaceAll(args[0], "-", "_")

		q, err := initQueue()
		failErr(err)

		val, err := q.Store().GetConfig(key)
		failErr(err)

		if val == "" {
			fmt.Fprintf(os.Stderr, "error: config key '%s' not found\n", key)
			os.Exit(1)
		}

		fmt.Println(val)
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all persistent configuration values",
	Run: func(cmd *cobra.Command, args []string) {
		q, err := initQueue()
		failErr(err)

		cfg, err := q.Store().GetAllConfig()
		failErr(err)

		if jsonOut {
			printJSON(cfg)
			return
		}

		for k, v := range cfg {
			fmt.Printf("%s=%s\n", k, v)
		}
	},
}

func init() {
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configListCmd)

	rootCmd.AddCommand(configCmd)
}
