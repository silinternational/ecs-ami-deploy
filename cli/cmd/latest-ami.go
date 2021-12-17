package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ead "github.com/silinternational/ecs-ami-deploy"
)

var AMIFilter string

// latestAMICmd represents the ec2 latest-ami command
var latestAMICmd = &cobra.Command{
	Use:   "latest-ami",
	Short: "Show latest AMI for filter in currently authenticated region",
	Long:  "Command returns a description of the latest AMI matching the given filter",
	Run: func(cmd *cobra.Command, args []string) {
		initAwsCfg()

		upgrader, err := ead.NewUpgrader(AwsCfg, &ead.Config{AMIFilter: AMIFilter})
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		latest, err := upgrader.LatestAMI()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		fmt.Printf("Latest AMI for filter \"%s\" is %s:\n\n", AMIFilter, *latest.ImageId)
		jb, _ := json.MarshalIndent(latest, "", "  ")
		fmt.Printf("%s", string(jb))
	},
}

func init() {
	rootCmd.AddCommand(latestAMICmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// ecsListInstanceIPsCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// ecsListInstanceIPsCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	// ecsReplaceInstancesCmd.Flags().StringVarP(&cluster, "cluster", "c", "", "ECS cluster name")
	latestAMICmd.Flags().StringVarP(&AMIFilter, "filter", "f", ead.DefaultAMIFilter, "AMI name filter")
}
