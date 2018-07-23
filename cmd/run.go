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
	"github.com/springload/ecs-tool/deploy"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Runs a command",
	Long: `Runs the specified command on an ECS cluster, optinally catching its output.

It can modify the container command.
`,
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

		exitCode, err := deploy.RunTask(
			viper.GetString("profile"),
			viper.GetString("cluster"),
			viper.GetString("task_definition"),
			containerName,
			viper.GetString("log_group"),
			commandArgs,
		)
		if err != nil {
			log.WithError(err).Error("Can't run task")
		}
		os.Exit(exitCode)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.PersistentFlags().StringP("log_group", "l", "", "Name of the log group to get output")
	runCmd.PersistentFlags().StringP("container_name", "", "", "Name of the container to modify parameters for")
	viper.BindPFlag("log_group", runCmd.PersistentFlags().Lookup("log_group"))
	viper.BindPFlag("container_name", runCmd.PersistentFlags().Lookup("container_name"))

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// runCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// runCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
