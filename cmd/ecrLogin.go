package cmd

import (
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/springload/ecs-tool/deploy"
)

// ecrLoginCmd represents the ecrLogin command
var ecrLoginCmd = &cobra.Command{
	Use:   "ecr-login",
	Short: "Gets command for docker login",
	Long: `Gets command for docker login.

Use it like so:
$eval $(ecs-tool ecr-login)
`,
	Run: func(cmd *cobra.Command, args []string) {
		err := deploy.EcrLogin(
			viper.GetString("profile"),
		)
		if err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(ecrLoginCmd)
}
