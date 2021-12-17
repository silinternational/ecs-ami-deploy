package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	ead "github.com/silinternational/ecs-ami-deploy"
	"github.com/spf13/cobra"
)

// latestAMICmd represents the ec2 latest-ami command
var listClustersCmd = &cobra.Command{
	Use:   "list-clusters",
	Short: "List all ECS Clusters and AMI status",
	Long:  "Command returns a list of ECS clusters along with AMI and is latest status",
	Run: func(cmd *cobra.Command, args []string) {
		listClusters()
	},
}

func init() {
	rootCmd.AddCommand(listClustersCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// ecsListInstanceIPsCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// ecsListInstanceIPsCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	// ecsReplaceInstancesCmd.Flags().StringVarP(&cluster, "cluster", "c", "", "ECS cluster name")
	// latestAMICmd.Flags().StringVarP(&AMIFilter, "filter", "f", ead.DefaultAMIFilter, "AMI name filter")
}

func listClusters() {
	initAwsCfg()

	upgrader, err := ead.NewUpgrader(AwsCfg, &ead.Config{AMIFilter: AMIFilter})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	latestAMI, err := upgrader.LatestAMI()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	list, err := upgrader.ListClusters()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Printf("\nLatest AMI: %s released %s\n\n", *latestAMI.Name, *latestAMI.CreationDate)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.Debug)
	_, _ = fmt.Fprintln(w, "Cluster \t Current AMI \t Released \t Is Latest AMI?")

	for _, c := range list {
		isLatest := *latestAMI.ImageId == *c.Image.ImageId
		_, _ = fmt.Fprintf(w, "%s \t %s \t %s \t %t\n", *c.Cluster.ClusterName, *c.Image.Name, *c.Image.CreationDate, isLatest)
	}
	_ = w.Flush()
	fmt.Println("")
}
