module github.com/silinternational/ecs-ami-deploy

go 1.13

require (
	github.com/aws/aws-lambda-go v1.27.1
	github.com/aws/aws-sdk-go-v2 v1.11.2
	github.com/aws/aws-sdk-go-v2/config v1.11.0
	github.com/aws/aws-sdk-go-v2/service/autoscaling v1.16.1
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.25.0
	github.com/aws/aws-sdk-go-v2/service/ecs v1.13.1
	github.com/mitchellh/go-homedir v1.1.0
	github.com/spf13/cobra v1.3.0
	github.com/spf13/viper v1.10.1
	golang.org/x/sys v0.1.0 // indirect
)
