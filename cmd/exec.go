package cmd

import (
    "os"

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
            containerName = args[0]
            commandArgs = args[1:]
        } else {
            containerName = name
            commandArgs = args
        }

        exitCode, err := lib.ExecFargate(
            viper.GetString("profile"),
            viper.GetString("cluster"),
            viper.GetString("run.service"),
            viper.GetString("workdir"),
            containerName,
            viper.GetString("log_group"),
            viper.GetString("run.launch_type"),
            viper.GetString("run.security_group_filter"),
            commandArgs,
        )
        if err != nil {
            log.WithError(err).Error("Can't run task in Fargate mode")
        }
        os.Exit(exitCode)
    },
}

func init() {
    rootCmd.AddCommand(execCmd)
}
