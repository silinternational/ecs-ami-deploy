# Automate EC2 instance replacement with updated ECS optimized AMI for ECS clusters

## What is this?
`ecs-ami-deploy` is a library of code for intelligently replacing instances in an autoscaling group (ASG) with
the latest [ECS Optimized](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs-optimized_AMI.html) image. 
Based on a couple assumptions, this library can replace all instances in an ECS cluster without causing downtime for 
any of the running services.

This process is available as:
 - A Go Module: `import "github.com/silinternational/ecs-ami-deploy/v2"`
 - A command line application. See `cli/` directory

## Why did we build this?
AWS releases new ECS optimized AMIs on a pretty frequent basis. It's generally a good idea to keep up to 
date with releases for security, performance, and other enhancements. However, replacing instances can be
a complicated dance if you also want to maintain a high level of availability for the services you're 
running on the instances to be replaced. AWS has a 
[feature to "refresh" instances](https://docs.aws.amazon.com/autoscaling/ec2/userguide/asg-instance-refresh.html) 
in an autoscaling group, but unfortunately it isn't "ECS aware". Rather, it removes EC2 instances from the autoscaling group, 
terminates them, and replaces them. When the new instances are "healthy" it moves on to replace more instances.
It does not take into account if the services you have running in ECS are stable yet when removing the next instance.
As a result, refreshing instances in an ASG using the built-in feature can and will likely cause downtime for your
ECS services. 

That isn't really an acceptable solution for us, so we created `ecs-ami-deploy` to refresh instances with
an awareness of the services running in ECS and to replace EC2 instances only when ECS services are stable. This
process does assume more than one task per service is running so that when an EC2 instance is removed, ECS
will launch a new task on a different instance while other tasks for this service remain where they are. Then 
one by one, EC2 instances are removed from service and terminated, and only when all services are stable again, that is
they have zero pending tasks, then the next EC2 instance can be removed from service and so forth. 

## Idempotency
Gracefully replacing instances can take some time, especially for clusters with many instances supporting them. The
process was designed to be fault-tolerant and to pick up where it left off should it terminate. One of the ways this is
accomplished is that all EC2 instances in the ASG are tagged before being detached and terminated. On successive runs 
the process looks for tagged instances for the given cluster that are no longer in service and continues the graceful
termination process while monitoring the ECS cluster services for stability.

If `--force-replacement` is enabled, the process will always replace all instances whether there is a newer AMI 
available or not. When `--force-replacement` is enabled the process is _not_ idempotent.  

## Instance Replacement Process

 1. Look up latest AMI based on either the given AMI filter, or the default: `amzn2-ami-ecs-hvm-*-x86_64-ebs`
 2. Identify the ASG for the given ECS cluster to get current launch template and instances list
 3. Compare latest AMI with AMI in use by launch template
    1. If cluster is not using latest AMI, or `force replacement` is enabled, proceed to #4
    2. Else if using latest AMI already, jump to #10
 4. Create new launch template version with new AMI
 5. Update launch template default version and set ASG to use the latest template version (`"$Latest"`)
 6. Detach existing instances from ASG and replace with new ones
 7. Wait for new instances to reach `InService` state with ASG
 8. Watch ECS cluster instances until all new ones are registered and available
 9. For each old instance that needs to be removed:
     1. Deregister one instance from ECS cluster
     2. Wait for zero pending tasks in cluster
     3. Terminate old ASG EC2 instance
 10. Scan all EC2 instances for any instances tagged for termination as part of this operation in case any 
     were missed on a previous run due to timeout or something else. For each:
     1. Terminate instance
     2. Wait for zero pending tasks in cluster
   
## Todo
 - [ ] Consistentify logging vs. returning errors
 - [ ] Add a logger that can send output to an email as well as stdout
 - [ ] Create Lambda wrapper and provide trigger examples for schedule and SNS when newer AMI is released

## CLI Usage
1. Grab the latest binary for your platform at https://github.com/silinternational/ecs-ami-deploy/releases
2. The CLI makes use of AWS's SDK for Go, which can load authentication credentials from various places similar to the 
   AWS CLI itself
3. Run `ecs-ami-deploy list-clusters` to check if it's working and what clusters you have available.
4. If you have multiple profiles configured in your `~/.aws/credentials` file, you can use the `-p` or `--profile` 
   flags to specify a different profile.
5. The CLI defaults to region `us-east-1`, you can use the `-r` or `--region` flags to specify a different region
6. The CLI has help information built in for the various subcommands and their supported flags, use `-h` or `--help` 
   flags with each subcommand for more information.
