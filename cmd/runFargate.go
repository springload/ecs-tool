package cmd

import (
	"os"

	"github.com/apex/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/springload/ecs-tool/lib"
)

var runCmd = &cobra.Command{
	Use:   "runFargate",
	Short: "Runs a command",
	Long: `Runs the specified command on an ECS cluster, optionally catching its output.

It can modify the container command.
`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var containerName string
		var commandArgs []string
		if name := viper.GetString("container_name"); name == "" {
			containerName = args[0]
			commandArgs = args[1:]
		} else {
			containerName = name
			commandArgs = args
		}

		exitCode, err := lib.RunTask(
			viper.GetString("profile"),
			viper.GetString("cluster"),
			viper.GetString("run.service"),
			viper.GetString("task_definition"),
			viper.GetString("image_tag"),
			viper.GetStringSlice("image_tags"),
			viper.GetString("workdir"),
			containerName,
			viper.GetString("log_group"),
			viper.GetString("run.launch_type"),
			commandArgs,
		)
		if err != nil {
			log.WithError(err).Error("Can't run task")
		}
		os.Exit(exitCode)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.PersistentFlags().StringP("log_group", "l", "", "Name of the log group to get output")
	runCmd.PersistentFlags().StringP("container_name", "", "", "Name of the container to modify parameters for")
	runCmd.PersistentFlags().StringP("task_definition", "t", "", "name of task definition to use (required)")
	viper.BindPFlag("log_group", runCmd.PersistentFlags().Lookup("log_group"))
	viper.BindPFlag("container_name", runCmd.PersistentFlags().Lookup("container_name"))
	viper.BindPFlag("task_definition", runCmd.PersistentFlags().Lookup("task_definition"))
	viper.SetDefault("run.launch_type", "EC2")
}
