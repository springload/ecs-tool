package cmd

import (
    "os"
    "strings"

    "github.com/apex/log"
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
    "github.com/springload/ecs-tool/lib"
)

var execCmd = &cobra.Command{
    Use:   "exec",
    Short: "Executes a command in an existing ECS Fargate container",

    Long: `Executes a specified command in a running container on an ECS Fargate cluster.`,
    Args: cobra.MinimumNArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        viper.SetDefault("run.launch_type", "FARGATE")
        //var containerName string
        var commandArgs []string
        if name := viper.GetString("container_name"); name == "" {
            commandArgs = args[1:]
        } else {
            commandArgs = args
        }

        // Join the commandArgs to form a single command string
        commandString := strings.Join(commandArgs, " ")

        err := lib.ExecFargate(
            viper.GetString("profile"),
            viper.GetString("cluster"),
            commandString, // Pass the combined command string
            viper.GetString("task_id"),         // Optional: will auto-extract task definition
            viper.GetString("task_definition"), // Optional: for extracting entrypoint
            viper.GetString("container_name"),  // Optional: for extracting entrypoint
        )
        if err != nil {
            log.WithError(err).Error("Can't execute command in Fargate mode")
            os.Exit(1)
        }
    },
}

func init() {
    rootCmd.AddCommand(execCmd)
    execCmd.PersistentFlags().StringP("task_id", "", "", "Task ID to use (will auto-extract task definition)")
    viper.BindPFlag("task_id", execCmd.PersistentFlags().Lookup("task_id"))
}
