package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	ead "github.com/silinternational/ecs-ami-deploy"
)

var (
	cluster                string
	forceReplace           bool
	launchConfigNamePrefix string
	launchConfigLimit      int
	pollingInterval        int
	pollingTimeout         int
)

// latestAMICmd represents the ec2 latest-ami command
var upgradeClusterCmd = &cobra.Command{
	Use:   "upgrade-cluster",
	Short: "Upgrade the ASG for the given ECS cluster to the latest AMI",
	Long:  "",
	Run: func(cmd *cobra.Command, args []string) {
		initAwsCfg()

		upgrader, err := ead.NewUpgrader(AwsCfg, &ead.Config{
			Cluster:                cluster,
			AMIFilter:              AMIFilter,
			ForceReplacement:       forceReplace,
			LaunchConfigNamePrefix: launchConfigNamePrefix,
			LaunchConfigLimit:      launchConfigLimit,
			PollingInterval:        time.Duration(pollingInterval) * time.Second,
			PollingTimeout:         time.Duration(pollingTimeout) * time.Minute,
		})
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if err := upgrader.UpgradeCluster(); err != nil {
			fmt.Printf("Error upgrading cluster: %s", err)
			os.Exit(1)
		}

		os.Exit(0)
	},
}

func init() {
	rootCmd.AddCommand(upgradeClusterCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// ecsListInstanceIPsCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// ecsListInstanceIPsCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	upgradeClusterCmd.PersistentFlags().StringVar(&cluster, "cluster", "", "Cluster name")
	_ = upgradeClusterCmd.MarkPersistentFlagRequired("cluster")

	upgradeClusterCmd.PersistentFlags().StringVar(&launchConfigNamePrefix, "launch-config-name-prefix", "", "Launch Configuration name prefix")
	upgradeClusterCmd.PersistentFlags().BoolVar(&forceReplace, "force-replacement", false, "Force replacement if current AMI is already latest")
	upgradeClusterCmd.PersistentFlags().StringVar(&AMIFilter, "ami-filter", ead.DefaultAMIFilter, "AMI search filter")
	upgradeClusterCmd.PersistentFlags().IntVar(&launchConfigLimit, "launch-config-limit", ead.DefaultLaunchConfigLimit, "Number of previous launch configurations to keep.")
	upgradeClusterCmd.PersistentFlags().IntVar(&pollingInterval, "polling-interval-seconds", int(ead.DefaultPollingInterval.Seconds()), "Number of seconds between status checks.")
	upgradeClusterCmd.PersistentFlags().IntVar(&pollingTimeout, "polling-timeout-minutes", int(ead.DefaultPollingTimeout.Minutes()), "Number of minutes before a polling operation times out.")
}
