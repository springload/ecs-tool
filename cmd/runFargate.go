package cmd

import (
    "os"

    "github.com/apex/log"
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
    "github.com/springload/ecs-tool/lib"
)

var runFargateCmd = &cobra.Command{
    Use:   "runFargate",
    Short: "Runs a command in Fargate mode",
    Long: `Runs the specified command on an ECS cluster, optionally catching its output.

This command is specifically tailored for future Fargate-specific functionality but currently duplicates the 'run' command.`,
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

        exitCode, err := lib.RunFargate(
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
    rootCmd.AddCommand(runFargateCmd)
  	viper.SetDefault("run.security_group_filter", "*ec2*")
  	viper.SetDefault("run.launch_type", "FARGATE")


}
