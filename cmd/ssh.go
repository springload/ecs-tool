package cmd

import (
	"os"

	"github.com/apex/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/springload/ecs-tool/deploy"
)

var sshCmd = &cobra.Command{
	Use:   "ssh",
	Short: "Get a shell",
	Long:  "Drops the user into a shell inside the application container",
	Run: func(cmd *cobra.Command, args []string) {
		container_name := viper.GetString("ssh.container_name")
		service := viper.GetString("ssh.service")
		if container_name == "" {
			container_name = service
		}
		exitCode, err := deploy.SSH(
			viper.GetString("profile"),
			viper.GetString("cluster"),
			viper.GetString("task_definition"),
			container_name,
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
