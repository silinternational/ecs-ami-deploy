package main

import (
	"fmt"

	"github.com/silinternational/ecs-ami-deploy/cli/cmd"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

func main() {
	fmt.Printf("ecs-ami-deploy version %s\n\n", version)
	cmd.Version = version
	cmd.Commit = commit
	cmd.Date = date
	cmd.BuiltBy = builtBy
	cmd.Execute()
}
