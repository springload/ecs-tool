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
			viper.GetString("ssh.task_definition"),
			containerName,
			viper.GetString("ssh.shell"),
			service,
			viper.GetString("ssh.instance_user"),
			viper.GetBool("ssh.push_ssh_key"),
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
	// Bind to sshCmd's own flag. runCmd does not define task_definition
	// (its BindPFlag call is commented out), so the lookup returned a
	// nil *pflag.Flag and any later viper.Get("ssh.task_definition")
	// call panicked inside pflagValue.HasChanged.
	viper.BindPFlag("ssh.task_definition", sshCmd.PersistentFlags().Lookup("task_definition"))

	viper.SetDefault("ssh.push_ssh_key", true)
	viper.SetDefault("ssh.task_definition", viper.GetString("task_definition"))
}
