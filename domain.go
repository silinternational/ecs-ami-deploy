package ead

import (
	"log"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	ecsTypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

const (
	DefaultAMIFilter           = "amzn2-ami-ecs-hvm-*-x86_64-ebs"
	DefaultPollingTimeout      = 15 * time.Minute
	DefaultPollingInterval     = 5 * time.Second
	DefaultLaunchTemplateLimit = 5
	DefaultTimestampLayout     = "20060102T150405"
	MinimumIntervalsForStable  = 6
	TagNameASG                 = "ecs-ami-deploy-asg"
	TagNameTerminate           = "ecs-ami-deploy-terminate"
	Version                    = "0.0.0"
)

type ClusterMeta struct {
	Cluster ecsTypes.Cluster
	Image   ec2types.Image
}

type Config struct {
	AMIFilter                string
	Cluster                  string
	ForceReplacement         bool
	LaunchTemplateLimit      int
	LaunchTemplateNamePrefix string
	Logger                   *log.Logger
	PollingInterval          time.Duration
	PollingTimeout           time.Duration
	TimestampLayout          string
}

var DefaultConfig = Config{
	AMIFilter:                DefaultAMIFilter,
	Cluster:                  "",
	ForceReplacement:         false,
	LaunchTemplateLimit:      DefaultLaunchTemplateLimit,
	LaunchTemplateNamePrefix: "",
	Logger:                   nil,
	PollingInterval:          DefaultPollingInterval,
	PollingTimeout:           DefaultPollingTimeout,
	TimestampLayout:          DefaultTimestampLayout,
}
