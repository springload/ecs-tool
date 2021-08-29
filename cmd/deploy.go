// Copyright © 2018 NAME HERE <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"os"

	"github.com/apex/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/springload/ecs-tool/lib"
)

// deployCmd represents the deploy command
var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Creates a new ECS Deployment",
	Long: `Creates a new ECS Deployment and checks the result.

If deployment failed, then rolls back to the previous stack definition.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if len(viper.GetStringSlice("deploy.services")) == 0 {
			log.Error("Can't deploy anything if no service is set")
			os.Exit(1)
		}

		exitCode, err := lib.DeployServices(
			viper.GetString("profile"),
			viper.GetString("cluster"),
			viper.GetString("image_tag"),
			viper.GetStringSlice("image_tags"),
			viper.GetStringSlice("deploy.services"),
		)
		if err != nil {
			log.WithError(err).Errorf("Deployment failed with code %d", exitCode)
		}
		os.Exit(exitCode)
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
	deployCmd.PersistentFlags().StringSliceP("service", "s", []string{}, "Names of services to update. Can be specified multiple times for parallel deployment")
	viper.BindPFlag("deploy.services", deployCmd.PersistentFlags().Lookup("service"))
}
