package main

import (
	"context"

	"github.com/aws/aws-lambda-go/lambda"
	ead "github.com/silinternational/ecs-ami-deploy"
)

func main() {
	lambda.Start(handler)
}

func handler(ctx context.Context, config ead.Config) error {
	return nil
}
