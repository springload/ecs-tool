package cmd

import (
	"strings"

	"github.com/apex/log"
	"github.com/spf13/cobra"
)

// envsCmd represents the envs command
var envsCmd = &cobra.Command{
	Use:   "envs",
	Short: "Discover available environments",
	Run: func(cmd *cobra.Command, args []string) {
		envs, err := findEnvironments()
		if err != nil {
			log.WithError(err).Fatal("No environments have been found")
		}
		log.Infof("Found following environments: %s", strings.Join(envs, ", "))
		log.Infof("Try running `ecs-tool run -e %s -- uptime`", envs[0])
	},
}

func init() {
	rootCmd.AddCommand(envsCmd)
}
