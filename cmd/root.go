package cmd

import (
	"fmt"
	"os"

	"github.com/apex/log"
	"github.com/apex/log/handlers/text"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile,
	environment string
	debug bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ecs-tool",
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
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file to use. Overrides -e/--environment lookup")
	rootCmd.PersistentFlags().StringVarP(&environment, "environment", "e", "", "look up config based on the environment flag. It looks for ecs-$environment.toml config in infra folder.")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Show debug output")
	rootCmd.PersistentFlags().StringP("cluster", "c", "", "name of cluster (required)")
	rootCmd.PersistentFlags().StringP("profile", "p", "", "name of AWS profile to use, which is set in ~/.aws/config")
	rootCmd.PersistentFlags().StringP("image_tag", "", "", "Overrides the docker image tag")

	viper.BindPFlag("profile", rootCmd.PersistentFlags().Lookup("profile"))
	viper.BindPFlag("cluster", rootCmd.PersistentFlags().Lookup("cluster"))
	viper.BindPFlag("image_tag", rootCmd.PersistentFlags().Lookup("image_tag"))

}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	log.SetHandler(text.New(os.Stdout))
	if debug {
		log.SetLevel(log.DebugLevel)
	}
	viper.SetEnvPrefix("ecs")
	viper.AutomaticEnv() // read in environment variables that match
	if cfgFile != "" || environment != "" {
		// Use config file from the flag. cfgFile takes precedence over environment
		if cfgFile != "" {
			viper.SetConfigFile(cfgFile)
		} else {
			if cfg, err := findConfigByEnvironment(environment); err != nil {
				log.WithError(err).Fatal("Can't find the config")
			} else {
				viper.SetConfigFile(cfg)
			}
		}
		// If a config file is found, read it in.
		if err := viper.ReadInConfig(); err == nil {
			log.Infof("Using config file: %s", viper.ConfigFileUsed())
		} else {
			log.WithError(err).Fatal("Had some errors while parsing the config")
			os.Exit(1)
		}
	}

}
