package cmd

import (
    "os"
    "github.com/apex/log"
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
    "github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/session"
    "github.com/aws/aws-sdk-go/service/ecs"
    "github.com/springload/ecs-tool/lib"
)

// execCmd executes a command in an existing ECS Fargate container.
var execCmd = &cobra.Command{
    Use:   "exec",
    Short: "Executes a command in an existing ECS Fargate container",
    Long: `Executes a specified command in a running container on an ECS Fargate cluster. 
           This command allows for interactive sessions and command execution in Fargate.`,
    Args: cobra.MinimumNArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        // Command to be executed within the container
        command := args[0]

        // Establish an AWS session using the specified profile and region from configuration
        sess, err := session.NewSessionWithOptions(session.Options{
            Profile: viper.GetString("profile"),
            Config: aws.Config{
                Region: aws.String(viper.GetString("region")),
            },
        })
        if err != nil {
            log.WithError(err).Error("Failed to create AWS session")
            os.Exit(1)
        }

        // Create a new ECS service client with the session
        svc := ecs.New(sess)

        // Execute the command in the specified ECS container using the ECS service client
        err = lib.ExecuteCommandInContainer(svc, viper.GetString("cluster"), viper.GetString("service_name"), viper.GetString("container_name"), command)
        if err != nil {
            log.WithError(err).Error("Failed to execute command in ECS Fargate container")
            os.Exit(1)
        } else {
            log.Info("Command executed successfully in ECS Fargate container")
            os.Exit(0)
        }
    },
}

func init() {
    rootCmd.AddCommand(execCmd)
    execCmd.Flags().String("profile", "", "AWS profile to use")
    execCmd.Flags().String("region", "", "AWS region to operate in")
    execCmd.Flags().String("cluster", "", "Name of the ECS cluster")
    execCmd.Flags().String("service_name", "", "Name of the ECS service")
    execCmd.Flags().String("container_name", "", "Name of the container in the task")
    
    viper.BindPFlag("profile", execCmd.Flags().Lookup("profile"))
    viper.BindPFlag("region", execCmd.Flags().Lookup("region"))
    viper.BindPFlag("cluster", execCmd.Flags().Lookup("cluster"))
    viper.BindPFlag("service_name", execCmd.Flags().Lookup("service_name"))
    viper.BindPFlag("container_name", execCmd.Flags().Lookup("container_name"))

    // Set default values or read from a configuration file
    viper.SetDefault("region", "us-east-1")
    viper.SetDefault("container_name", "default-container")
}
