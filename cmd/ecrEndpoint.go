package cmd

import (
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/springload/ecs-tool/lib"
)

// ecrEndpointCmd represents the ecrEndpoint command
var ecrEndpointCmd = &cobra.Command{
	Use:   "ecr-endpoint",
	Short: "Outputs the ECR endpoint",
	Long: `
Prints the ECR endpoint, which is constructed as {account_number}.dkr.ecr.{region}.amazonaws.com
`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := lib.EcrEndpoint(
			viper.GetString("profile"),
		); err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(ecrEndpointCmd)
}
