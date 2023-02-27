package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// aboutCmd displays information about this build of ecs-ami-deploy
var aboutCmd = &cobra.Command{
	Use:   "about",
	Short: "Display information about this build of ecs-ami-deploy",
	Long:  "",
	Run: func(cmd *cobra.Command, args []string) {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.Debug)
		_, _ = fmt.Fprintf(w, "Version:\t %s\n", Version)
		_, _ = fmt.Fprintf(w, "Commit:\t %s\n", Commit)
		_, _ = fmt.Fprintf(w, "Build Date:\t %s\n", Date)
		_, _ = fmt.Fprintf(w, "Built By:\t %s\n", BuiltBy)

		_ = w.Flush()
		fmt.Println("")
	},
}

func init() {
	rootCmd.AddCommand(aboutCmd)
}
