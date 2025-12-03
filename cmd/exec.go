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
        var containerName string
        var commandArgs []string
        if name := viper.GetString("container_name"); name == "" {
            // If container_name not provided via flag, use first arg as container name
            if len(args) < 2 {
                log.Error("When --container_name is not provided, at least 2 arguments are required: <container_name> <command>")
                os.Exit(1)
            }
            containerName = args[0]
            commandArgs = args[1:]
        } else {
            containerName = name
            commandArgs = args
        }

        // Validate that we have a command to execute
        if len(commandArgs) == 0 {
            log.Error("No command provided to execute")
            os.Exit(1)
        }

        // Join the commandArgs to form a single command string
        commandString := strings.Join(commandArgs, " ")

        err := lib.ExecFargate(lib.ExecConfig{
            Profile:            viper.GetString("profile"),
            Cluster:            viper.GetString("cluster"),
            Command:            commandString,
            TaskID:             viper.GetString("task_id"),
            TaskDefinitionName: viper.GetString("task_definition"),
            ContainerName:      containerName,
        })
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
