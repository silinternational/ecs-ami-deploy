package cmd

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"

	"github.com/spf13/cobra"

	"github.com/spf13/viper"
)

var (
	AwsCfg  aws.Config
	cfgFile string
	Profile string
	Region  string

	// The following vars are updated by build process

	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
	BuiltBy = "unknown"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ecs-ami-deploy",
	Short: "Rotate AMI used in ASG for latest ECS Optimized image",
	Long: `A CLI, library, and Lambda function for rotating instances in an auto-scaling group with
an updated ECS optimized image`,
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

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.ecs-ami-deploy.yaml)")
	rootCmd.PersistentFlags().StringVarP(&Profile, "profile", "p", "", "AWS shared credentials profile to use")
	rootCmd.PersistentFlags().StringVarP(&Region, "region", "r", "us-east-1", "AWS region")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Search config in home directory with name ".ecs-ami-deploy" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".ecs-ami-deploy")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func initAwsCfg() {
	var cfgOpts []func(options *config.LoadOptions) error

	if cfgFile != "" {
		cfgOpts = append(cfgOpts, config.WithSharedConfigFiles([]string{cfgFile}))
	}
	if Profile != "" {
		cfgOpts = append(cfgOpts, config.WithSharedConfigProfile(Profile))
	}
	if Region != "" {
		cfgOpts = append(cfgOpts, config.WithRegion(Region))
	}

	var err error
	AwsCfg, err = config.LoadDefaultConfig(context.TODO(), cfgOpts...)
	if err != nil {
		log.Printf("failed to load config with profile %s", Profile)
	}
}
