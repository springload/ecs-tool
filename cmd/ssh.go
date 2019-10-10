package cmd

import (
	"os"

	"github.com/apex/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/springload/ecs-tool/lib"
)

var sshCmd = &cobra.Command{
	Use:   "ssh",
	Short: "Get a shell",
	Long:  "Drops the user into a shell inside the application container",
	Run: func(cmd *cobra.Command, args []string) {
		containerName := viper.GetString("ssh.container_name")
		service := viper.GetString("ssh.service")
		if containerName == "" {
			containerName = service
		}
		exitCode, err := lib.ConnectSSH(
			viper.GetString("profile"),
			viper.GetString("cluster"),
			viper.GetString("task_definition"),
			containerName,
			viper.GetString("ssh.shell"),
			service,
			viper.GetString("ssh.instance_user"),
		)
		if err != nil {
			log.WithError(err).Error("Can't execute ssh")
		}
		os.Exit(exitCode)
	},
}

func init() {
	rootCmd.AddCommand(sshCmd)
	sshCmd.PersistentFlags().StringP("task_definition", "t", "", "name of task definition to use (required)")
	viper.BindPFlag("task_definition", runCmd.PersistentFlags().Lookup("task_definition"))
}
