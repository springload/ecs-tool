package cmd

import (
	"fmt"
	"os"

	"github.com/apex/log"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile,
	profile,
	task,
	cluster string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ecs-deploy",
	Short: "Deploys stuff on Elastic Container Service",
	Long: `This tool helps you create native ECS deployments, track if they are successfull and roll
back if needed.

It allows running one-off commands and get the output instantly.
`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.ecs-deploy.yaml)")
	rootCmd.PersistentFlags().StringVarP(&cluster, "cluster", "c", "", "Name of cluster (required)")
	rootCmd.PersistentFlags().StringVarP(&profile, "profile", "p", "", "Name of profile to use")
	rootCmd.PersistentFlags().StringVarP(&task, "task_definition", "t", "", "Name of task definition to use (required)")

	viper.BindPFlag("profile", rootCmd.PersistentFlags().Lookup("profile"))
	viper.BindPFlag("cluster", rootCmd.PersistentFlags().Lookup("cluster"))
	viper.BindPFlag("task_definition", rootCmd.PersistentFlags().Lookup("task_definition"))

}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Search config in home directory with name ".ecs-deploy" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".ecs-deploy")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		log.Infof("Using config file: %s", viper.ConfigFileUsed())
	} else {
		fmt.Println("Had some errors while parsing the config:", err)
		os.Exit(1)
	}
}
