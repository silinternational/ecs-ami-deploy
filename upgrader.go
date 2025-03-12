package ead

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/silinternational/ecs-ami-deploy/v2/internal"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	asgTypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecsTypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

type Upgrader struct {
	amiFilter                string
	cluster                  string
	forceReplacement         bool
	launchTemplateLimit      int
	launchTemplateNamePrefix string
	logger                   *log.Logger
	pollingInterval          time.Duration
	pollingTimeout           time.Duration
	timestampLayout          string

	awsCfg    aws.Config
	asgClient *autoscaling.Client
	ec2Client *ec2.Client
	ecsClient *ecs.Client
}

func NewUpgrader(awsCfg aws.Config, config *Config) (*Upgrader, error) {
	if awsCfg.Region == "" {
		return nil, fmt.Errorf("awsCfg must be initialized before use")
	}

	upgrader := &Upgrader{
		awsCfg: awsCfg,
	}

	if config == nil {
		config = &DefaultConfig
	}

	if err := upgrader.loadConfig(config); err != nil {
		return nil, fmt.Errorf("error loading config: %s", err)
	}

	upgrader.asgClient = autoscaling.NewFromConfig(awsCfg)
	upgrader.ec2Client = ec2.NewFromConfig(awsCfg)
	upgrader.ecsClient = ecs.NewFromConfig(awsCfg)

	return upgrader, nil
}

func (u *Upgrader) loadConfig(config *Config) error {
	if config.AMIFilter == "" {
		config.AMIFilter = DefaultConfig.AMIFilter
	}
	if config.Logger == nil {
		config.Logger = log.Default()
		config.Logger.SetOutput(os.Stdout)
	}
	if config.LaunchTemplateNamePrefix == "" {
		config.LaunchTemplateNamePrefix = "ecs-" + config.Cluster
	}
	if config.LaunchTemplateLimit == 0 {
		config.LaunchTemplateLimit = DefaultConfig.LaunchTemplateLimit
	}
	if config.PollingInterval == 0 {
		config.PollingInterval = DefaultConfig.PollingInterval
	}
	if config.PollingTimeout == 0 {
		config.PollingTimeout = DefaultConfig.PollingTimeout
	}
	if config.TimestampLayout == "" {
		config.TimestampLayout = DefaultConfig.TimestampLayout
	}

	u.amiFilter = config.AMIFilter
	u.cluster = config.Cluster
	u.forceReplacement = config.ForceReplacement
	u.launchTemplateLimit = config.LaunchTemplateLimit
	u.launchTemplateNamePrefix = config.LaunchTemplateNamePrefix
	u.logger = config.Logger
	u.pollingInterval = config.PollingInterval
	u.pollingTimeout = config.PollingTimeout
	u.timestampLayout = config.TimestampLayout

	return nil
}

// LatestAMI finds the latest ECS optimized AMI for the given (or default) filter
// and returns the ec2types.Image and/or an error
func (u *Upgrader) LatestAMI() (ec2types.Image, error) {
	descInput := &ec2.DescribeImagesInput{
		Filters: []ec2types.Filter{
			{
				Name: aws.String("name"),
				Values: []string{
					u.amiFilter,
				},
			},
		},
		Owners: []string{"amazon"},
	}

	output, err := u.ec2Client.DescribeImages(context.Background(), descInput)
	if err != nil {
		return ec2types.Image{}, err
	}

	if len(output.Images) == 0 {
		return ec2types.Image{}, nil
	}

	newest := output.Images[0]
	for _, img := range output.Images {
		isNewer, err := isNewerImage(newest, img)
		if err != nil {
			return ec2types.Image{}, err
		}
		if isNewer {
			newest = img
		}
	}

	return newest, nil
}

func (u *Upgrader) ListClusters() ([]ClusterMeta, error) {
	var allClusters []ClusterMeta
	var clusterARNs []string
	clustersPaginator := ecs.NewListClustersPaginator(u.ecsClient, &ecs.ListClustersInput{MaxResults: aws.Int32(100)})
	for clustersPaginator.HasMorePages() {
		page, err := clustersPaginator.NextPage(context.Background())
		if err != nil {
			return []ClusterMeta{}, err
		}
		clusterARNs = append(clusterARNs, page.ClusterArns...)

		descInput := &ecs.DescribeClustersInput{
			Clusters: clusterARNs,
		}

		results, err := u.ecsClient.DescribeClusters(context.Background(), descInput)
		if err != nil {
			return []ClusterMeta{}, fmt.Errorf("error describing clusters: %s", err)
		}

		for _, c := range results.Clusters {
			asg, err := u.getAsgNameForCluster(*c.ClusterName)
			if err != nil {
				// if error, include in list but don't attempt to fetch more information
				allClusters = append(allClusters, ClusterMeta{
					Cluster: c,
					Image: ec2types.Image{
						CreationDate: aws.String("na"),
						ImageId:      aws.String("na"),
						Name:         aws.String(fmt.Sprintf("%s", err.Error())),
					},
				})
				continue
			}

			_, ltData, err := u.getLaunchTemplateForASG(asg)
			if err != nil {
				fmt.Printf("Error getting launch template for cluster %s\n%s\n\n", *c.ClusterName, err)
				continue
			}
			img, err := u.getImageByID(*ltData.ImageId)
			if err != nil {
				return []ClusterMeta{}, fmt.Errorf("error getting image details for cluster %s: %s", *c.ClusterName, err)
			}

			allClusters = append(allClusters, ClusterMeta{
				Cluster: c,
				Image:   img,
			})
		}
	}

	return allClusters, nil
}

func (u *Upgrader) UpgradeCluster() error {
	if u.cluster == "" {
		return fmt.Errorf("cluster name must be set in config for upgrade")
	}

	if u.forceReplacement {
		u.logger.Println("force-replacement is enabled so this run is not idempotent")
	}

	startTime := time.Now()
	u.logger.Printf("Beginning upgrade for ECS cluster %s using AMI filter %s\n", u.cluster, u.amiFilter)

	asgName, err := u.getAsgNameForCluster(u.cluster)
	if err != nil {
		return err
	}
	u.logger.Printf("Found ASG: %s\n", asgName)

	lt, ltData, err := u.getLaunchTemplateForASG(asgName)
	if err != nil {
		return err
	}
	u.logger.Printf("Launch template: %s\n", *lt.LaunchTemplateName)
	u.logger.Printf("Latest version: %d\n", *lt.LatestVersionNumber)
	u.logger.Printf("Current image ID: %s\n", *ltData.ImageId)

	latestImage, err := u.LatestAMI()
	if err != nil {
		return err
	}
	u.logger.Printf("Latest image found: %s\n", *latestImage.ImageId)

	current, err := u.getImageByID(*ltData.ImageId)
	if err != nil {
		return err
	}

	isNewer, err := isNewerImage(current, latestImage)
	if err != nil {
		return err
	}

	if !isNewer && !u.forceReplacement {
		u.logger.Println("Upgrade not needed, cluster is already running latest AMI")
		return u.terminateOrphanedInstances(asgName)
	}

	if u.forceReplacement {
		u.logger.Println("Cluster already running latest AMI, but replacing instances anyway")
	} else {
		u.logger.Println("Latest image determined to be newer than image currently in use, proceeding with upgrade")
	}

	asg, err := u.getAsgByName(asgName)
	if err != nil {
		return fmt.Errorf("failed to get ASG by name: %s", err)
	}

	// get cluster list before new instances are added
	originalClusterInstances, err := u.getInstanceListForCluster(u.cluster)
	if err != nil {
		return err
	}
	if len(originalClusterInstances) == 0 {
		return fmt.Errorf("no container instances found in cluster")
	}
	originalInstanceIDs := make([]string, len(originalClusterInstances))
	for i, instance := range originalClusterInstances {
		originalInstanceIDs[i] = *instance.Ec2InstanceId
	}
	u.logger.Printf("Existing instances in ASG: %s\n", strings.Join(originalInstanceIDs, ", "))

	newLtv, err := u.newLaunchTemplateVersionWithNewImage(lt, ltData, latestImage)
	if err != nil {
		return err
	}
	u.logger.Printf("New launch template version created: %d\n", *newLtv.VersionNumber)

	if err := u.updateAsgLaunchTemplate(asgName, newLtv); err != nil {
		return err
	}
	u.logger.Println("ASG updated to use new launch template version")

	// detach and replace instances
	if err := u.detachAndReplaceAsgInstances(asgName); err != nil {
		return err
	}

	// watch ECS cluster for new EC2 instances to be registered
	if err := u.waitForContainerInstanceCount(u.cluster, int(*asg.DesiredCapacity)*2); err != nil {
		return err
	}

	clusterInstances, err := u.getInstanceIDsForCluster(u.cluster)
	if err != nil {
		return err
	}

	var newInstances []string
	for _, c := range clusterInstances {
		if !internal.IsStringInSlice(c, originalInstanceIDs) {
			newInstances = append(newInstances, c)
		}
	}
	u.logger.Printf("New instances in ASG: %s\n", strings.Join(newInstances, ", "))

	// Terminate existing instances one at a time while waiting for services to stabilize after each
	for _, i := range originalClusterInstances {
		if err := u.deregisterClusterInstance(*i.ContainerInstanceArn, u.cluster); err != nil {
			return err
		}

		if err := u.safeTerminateInstance(*i.Ec2InstanceId); err != nil {
			return err
		}
	}

	if err := u.terminateOrphanedInstances(asgName); err != nil {
		return err
	}

	if err := u.cleanupOldLaunchTemplates(); err != nil {
		return err
	}

	u.logger.Printf("Upgrade cluster process completed in %s", time.Since(startTime))

	return nil
}

func (u *Upgrader) getAsgNameForCluster(cluster string) (string, error) {
	instanceIDs, err := u.getInstanceIDsForCluster(cluster)
	if err != nil {
		return "", err
	}

	if len(instanceIDs) == 0 {
		return "", fmt.Errorf("no instances found for cluster %s", cluster)
	}

	instanceDetails, err := u.ec2Client.DescribeInstances(context.Background(), &ec2.DescribeInstancesInput{
		InstanceIds: instanceIDs,
	})
	if err != nil {
		return "", fmt.Errorf("unable to get asg name from instance: %s", err)
	}

	if len(instanceDetails.Reservations) == 0 {
		return "", fmt.Errorf("unable to find asg for ecs cluster, no instances returned in response")
	}

	for _, r := range instanceDetails.Reservations {
		for _, i := range r.Instances {
			for _, t := range i.Tags {
				if *t.Key == "aws:autoscaling:groupName" {
					return *t.Value, nil
				}
			}
		}
	}

	return "", fmt.Errorf("after checking all instances in ecs cluster, no ASG tag name found")
}

func (u *Upgrader) getInstanceIDsForCluster(cluster string) ([]string, error) {
	instances, err := u.getInstanceListForCluster(cluster)
	if err != nil {
		return []string{}, err
	}

	var instanceIDs []string
	for _, instance := range instances {
		instanceIDs = append(instanceIDs, *instance.Ec2InstanceId)
	}

	return instanceIDs, nil
}

func (u *Upgrader) getInstanceListForCluster(cluster string) ([]ecsTypes.ContainerInstance, error) {
	listResult, err := u.ecsClient.ListContainerInstances(context.Background(), &ecs.ListContainerInstancesInput{
		Cluster: aws.String(cluster),
	})
	if err != nil {
		return []ecsTypes.ContainerInstance{}, fmt.Errorf("failed to list container instances: %s", err)
	}

	// if there are no instances in this cluster, return
	if len(listResult.ContainerInstanceArns) == 0 {
		return nil, nil
	}

	descResult, err := u.ecsClient.DescribeContainerInstances(context.Background(), &ecs.DescribeContainerInstancesInput{
		Cluster:            aws.String(cluster),
		ContainerInstances: listResult.ContainerInstanceArns,
	})
	if err != nil {
		return []ecsTypes.ContainerInstance{}, fmt.Errorf("failed to describe container instances: %s", err)
	}

	return descResult.ContainerInstances, nil
}

func (u *Upgrader) getLaunchTemplateForASG(asgName string) (*ec2types.LaunchTemplate, *ec2types.ResponseLaunchTemplateData, error) {
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{
			asgName,
		},
	}

	result, err := u.asgClient.DescribeAutoScalingGroups(context.Background(), input)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to describe auto-scaling groups: %w", err)
	}

	var group asgTypes.AutoScalingGroup

	// we should only get one ASG back, but just to be safe, loop through results and look for specific match
	for _, a := range result.AutoScalingGroups {
		if *a.AutoScalingGroupName == asgName {
			group = a
			break
		}
	}

	// if no matching group was found, return err
	if group.LaunchTemplate == nil {
		return nil, nil, fmt.Errorf("ASG %s has no launch template", asgName)
	}

	ltInput := &ec2.DescribeLaunchTemplatesInput{
		LaunchTemplateNames: []string{
			*group.LaunchTemplate.LaunchTemplateName,
		},
	}

	ltResult, err := u.ec2Client.DescribeLaunchTemplates(context.Background(), ltInput)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to describe launch templates: %w", err)
	}

	var lt *ec2types.LaunchTemplate

	// we should only get one LT back, but just to be safe, loop through results and look for specific match
	for _, l := range ltResult.LaunchTemplates {
		if *l.LaunchTemplateName == *group.LaunchTemplate.LaunchTemplateName {
			lt = &l
			break
		}
	}

	// if no matching LT was found, return err
	if lt == nil {
		return nil, nil, fmt.Errorf("unable to find a launch template by name %s for ASG %s",
			*group.LaunchTemplate.LaunchTemplateName, asgName)
	}

	ltdInput := ec2.DescribeLaunchTemplateVersionsInput{
		LaunchTemplateId: lt.LaunchTemplateId,
		Versions:         []string{"$Latest"},
	}
	ltv, err := u.ec2Client.DescribeLaunchTemplateVersions(context.Background(), &ltdInput)
	if err != nil {
		return nil, nil, err
	}

	return lt, ltv.LaunchTemplateVersions[0].LaunchTemplateData, nil
}

func (u *Upgrader) getImageByID(imageID string) (ec2types.Image, error) {
	imgInput := &ec2.DescribeImagesInput{
		ImageIds: []string{
			imageID,
		},
	}
	imgResult, err := u.ec2Client.DescribeImages(context.Background(), imgInput)
	if err != nil {
		return ec2types.Image{}, fmt.Errorf("failed to describe image by id: %s", err)
	}

	// should only get one image back, but to be safe loop through results to find match
	for _, i := range imgResult.Images {
		if *i.ImageId == imageID {
			return i, nil
		}
	}

	return ec2types.Image{}, fmt.Errorf("unable to find image by ID %s", imageID)
}

func (u *Upgrader) newLaunchTemplateVersionWithNewImage(lt *ec2types.LaunchTemplate,
	ltd *ec2types.ResponseLaunchTemplateData, image ec2types.Image) (*ec2types.LaunchTemplateVersion, error) {

	newLtd, err := makeLaunchTemplateDataRequest(ltd)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new launch template version, %w", err)
	}

	newLtd.ImageId = image.ImageId

	// KernelId and RamdiskId must be updated anytime a the ImageId is updated
	newLtd.KernelId = image.KernelId
	newLtd.RamDiskId = image.RamdiskId

	// need to nil out snapshot ids of block devices so they don't reference old AMI
	for _, b := range newLtd.BlockDeviceMappings {
		b.Ebs.SnapshotId = nil
	}

	// If newLtv has an SSH key name and it's empty, change to nil as empty is not valid
	if newLtd.KeyName != nil && *newLtd.KeyName == "" {
		newLtd.KeyName = nil
	}

	newLtv := ec2.CreateLaunchTemplateVersionInput{
		LaunchTemplateId:   lt.LaunchTemplateId,
		LaunchTemplateData: newLtd,
	}

	out, err := u.ec2Client.CreateLaunchTemplateVersion(context.Background(), &newLtv)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new launch template version, %w", err)
	}

	return out.LaunchTemplateVersion, nil
}

func makeLaunchTemplateDataRequest(in *ec2types.ResponseLaunchTemplateData) (*ec2types.RequestLaunchTemplateData, error) {
	var out ec2types.RequestLaunchTemplateData
	if err := internal.ConvertToOtherType(in, &out); err != nil {
		return nil, fmt.Errorf("error making launch template data for request, %w", err)
	}
	return &out, nil
}

func (u *Upgrader) updateAsgLaunchTemplate(asgName string, v *ec2types.LaunchTemplateVersion) error {
	updateInput := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(asgName),
		LaunchTemplate: &asgTypes.LaunchTemplateSpecification{
			LaunchTemplateId: v.LaunchTemplateId,
			Version:          aws.String("$Latest"),
		},
	}
	if _, err := u.asgClient.UpdateAutoScalingGroup(context.Background(), updateInput); err != nil {
		return fmt.Errorf("unable to update ASG %s to use launch template %s version %d, error: %w",
			asgName, *v.LaunchTemplateName, *v.VersionNumber, err)
	}

	in := &ec2.ModifyLaunchTemplateInput{
		DefaultVersion:   aws.String(fmt.Sprintf("%d", *v.VersionNumber)),
		LaunchTemplateId: v.LaunchTemplateId,
	}
	if _, err := u.ec2Client.ModifyLaunchTemplate(context.Background(), in); err != nil {
		return fmt.Errorf("failed to modify launch template: %w", err)
	}
	return nil
}

func (u *Upgrader) detachAndReplaceAsgInstances(asgName string) error {
	asg, err := u.getAsgByName(asgName)
	if err != nil {
		return fmt.Errorf("error trying to get ASG by name: %s", err)
	}

	var existingInstances []string
	for _, i := range asg.Instances {
		existingInstances = append(existingInstances, *i.InstanceId)
	}

	u.logger.Println("Tagging existing instances for later verification that they have been terminated")
	if err := u.tagInstancesForTermination(asgName, existingInstances); err != nil {
		return err
	}

	u.logger.Printf("Found %v existing instances in ASG", len(existingInstances))
	u.logger.Println("Detaching and replacing existing instances...")
	_, err = u.asgClient.DetachInstances(context.Background(), &autoscaling.DetachInstancesInput{
		AutoScalingGroupName:           &asgName,
		InstanceIds:                    existingInstances,
		ShouldDecrementDesiredCapacity: aws.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("error trying to detach existing instances: %s", err)
	}

	u.logger.Printf("Existing instances detached, new instances starting soon, will wait up to %s", u.pollingTimeout)
	return u.waitForNewAsgInstances(asgName)
}

func (u *Upgrader) getAsgByName(asgName string) (*asgTypes.AutoScalingGroup, error) {
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{
			asgName,
		},
	}

	result, err := u.asgClient.DescribeAutoScalingGroups(context.Background(), input)
	if err != nil {
		return nil, fmt.Errorf("error trying to describe auto-scaling groups: %s", err)
	}

	var asg *asgTypes.AutoScalingGroup
	for _, g := range result.AutoScalingGroups {
		if *g.AutoScalingGroupName == asgName {
			asg = &g
			break
		}
	}

	if asg == nil {
		return nil, fmt.Errorf("ASG with name %s not found", asgName)
	}

	return asg, nil
}

// tagInstancesForTermination - Prior to detaching instances from their ASG, we tag them
// with the ASG name so on subsequent runs we can detect detached instances that have not
// been terminated. This allows for rerunning after process errors or is killed due to timeout.
func (u *Upgrader) tagInstancesForTermination(asgName string, instanceIDs []string) error {
	input := &ec2.CreateTagsInput{
		Resources: instanceIDs,
		Tags: []ec2types.Tag{
			{
				Key:   aws.String(TagNameASG),
				Value: aws.String(asgName),
			},
			{
				Key:   aws.String(TagNameTerminate),
				Value: aws.String("true"),
			},
		},
	}

	_, err := u.ec2Client.CreateTags(context.Background(), input)
	if err != nil {
		return fmt.Errorf("failed to tag instances for termination: %s", err)
	}

	return nil
}

func (u *Upgrader) waitForNewAsgInstances(asgName string) error {
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{asgName},
	}

	// the provided InService waiter in SDK doesn't seem to work. Had to write an overriding Retryable
	// feature to get desired results.
	waiter := autoscaling.NewGroupInServiceWaiter(u.asgClient, func(options *autoscaling.GroupInServiceWaiterOptions) {
		options.Retryable = func(ctx context.Context, input *autoscaling.DescribeAutoScalingGroupsInput, output *autoscaling.DescribeAutoScalingGroupsOutput, err error) (bool, error) {
			if output == nil {
				return true, nil
			}

			for _, a := range output.AutoScalingGroups {
				// we should only get one ASG back, but just in case, compare the name
				if *a.AutoScalingGroupName != asgName {
					continue
				}

				inServiceCount := int32(0)
				for _, i := range a.Instances {
					if i.LifecycleState == asgTypes.LifecycleStateInService {
						inServiceCount++
					}
				}

				// if we have at least as many in service instances as the minimum size of the ASG, consider it ready
				if inServiceCount >= *a.MinSize {
					return false, nil
				}

				u.logger.Printf("ASG not ready yet, min size = %v, currently in service = %v", *a.MinSize, inServiceCount)
				return true, nil
			}

			// if we're here, it must be that the output did not include the ASG we're looking for, so retry
			return true, nil
		}
	})
	if err := waiter.Wait(context.Background(), input, u.pollingTimeout); err != nil {
		return fmt.Errorf("error waiting for ASG to become in service after detaching instances: %s", err)
	}

	u.logger.Println("All new ASG instances in ready state")
	return nil
}

func (u *Upgrader) waitForContainerInstanceCount(cluster string, desired int) error {
	input := &ecs.DescribeClustersInput{
		Clusters: []string{cluster},
	}

	u.logger.Printf("Waiting for cluster %s instances...", cluster)
	startTime := time.Now()
	for {
		if time.Since(startTime) >= u.pollingTimeout {
			return fmt.Errorf("timeout while waiting for cluster %s to have %v instances", cluster, desired)
		}
		time.Sleep(u.pollingInterval)

		result, err := u.ecsClient.DescribeClusters(context.Background(), input)
		if err != nil {
			return fmt.Errorf("error describing cluster: %s", err)
		}

		// we should only get one cluster back, but loop and check name to be sure
		for _, c := range result.Clusters {
			if *c.ClusterName != cluster {
				continue
			}
			if c.RegisteredContainerInstancesCount == int32(desired) {
				u.logger.Printf("Cluster %s now has %v registered instances.", cluster, c.RegisteredContainerInstancesCount)
				return nil
			}
			u.logger.Printf("Still waiting for cluster %s to have %v registered instances, currently has %v", cluster, desired, c.RegisteredContainerInstancesCount)
		}
	}
}

func (u *Upgrader) deregisterClusterInstance(clusterInstanceArn, cluster string) error {
	input := &ecs.DeregisterContainerInstanceInput{
		ContainerInstance: aws.String(clusterInstanceArn),
		Cluster:           aws.String(cluster),
		Force:             aws.Bool(true),
	}

	u.logger.Printf("Deregistering cluster instance %s...", clusterInstanceArn)
	_, err := u.ecsClient.DeregisterContainerInstance(context.Background(), input)
	if err != nil {
		return fmt.Errorf("error deregistering instance from cluster %s: %s", cluster, err)
	}

	return nil
}

func (u *Upgrader) safeTerminateInstance(instanceId string) error {
	// before terminating instance ensure cluster is stable
	u.logger.Println("Waiting for services to stabilize...")
	if err := u.waitForStableCluster(); err != nil {
		return err
	}
	u.logger.Printf("Services stable, will terminate instance %s now", instanceId)

	if err := u.terminateInstances([]string{instanceId}); err != nil {
		return err
	}

	// before returning, wait again for stable cluster
	return u.waitForStableCluster()
}

// waitForStableCluster monitors the pending task count for the cluster and waits for it to
// reach zero and stay at zero for the configured number of interval checks in case a task
// fails to stay running
func (u *Upgrader) waitForStableCluster() error {
	input := &ecs.DescribeClustersInput{
		Clusters: []string{u.cluster},
	}

	// track how many iterations we see zero pending tasks
	stableCheckCount := 0

	startTime := time.Now()
	for {
		if stableCheckCount >= MinimumIntervalsForStable {
			// we've seen zero pending tasks for MinimumIntervalsForStable iterations,
			// as extra safety precaution make sure there are no pending or incomplete deployments
			u.logger.Println("Waiting for all service deployments to complete...")
			return u.waitForCompletedDeployments()
		}

		if time.Since(startTime) >= u.pollingTimeout {
			return fmt.Errorf("timeout while waiting for cluster to stabilize")
		}
		time.Sleep(u.pollingInterval)

		result, err := u.ecsClient.DescribeClusters(context.Background(), input)
		if err != nil {
			return fmt.Errorf("error checking cluster status: %s", err)
		}

		for _, c := range result.Clusters {
			// we should only get one cluster back, but just in case compare the name
			if *c.ClusterName != u.cluster {
				continue
			}

			if c.PendingTasksCount == 0 {
				stableCheckCount++
				u.logger.Printf("Cluster appears to be stable, iteration count %v of %v", stableCheckCount, MinimumIntervalsForStable)
				continue
			}
			stableCheckCount = 0
			u.logger.Printf("Waiting on %v pending tasks", c.PendingTasksCount)
		}
	}
}

func (u *Upgrader) waitForCompletedDeployments() error {
	serviceArns, err := u.listServiceARNs()
	if err != nil {
		return fmt.Errorf("error getting list of service arns: %s", err)
	}

	u.logger.Printf("Found %v services to monitor status", len(serviceArns))

	// Can only include 10 services per page of requests to DescribeServices, so chunk into pages
	pageSize := 10
	pages := int(math.Ceil(float64(len(serviceArns)) / float64(pageSize)))

	for p := 0; p < pages; p++ {
		start := p * pageSize
		end := start + pageSize
		var pageServiceArns []string
		if end <= len(serviceArns) {
			pageServiceArns = serviceArns[start:end]
		} else {
			pageServiceArns = serviceArns[start:]
		}

		serviceDeploymentComplete := make(map[string]bool, len(pageServiceArns))
		for _, arn := range pageServiceArns {
			serviceDeploymentComplete[arn] = false
		}

		input := &ecs.DescribeServicesInput{
			Cluster:  aws.String(u.cluster),
			Services: pageServiceArns,
		}

		startTime := time.Now()
		u.logger.Printf("Monitoring deployment status for services: %s\n", strings.Join(pageServiceArns, ", "))

	loop:
		for {
			if time.Since(startTime) > u.pollingTimeout {
				return fmt.Errorf("timeout while waiting for completed deployments")
			}
			time.Sleep(u.pollingInterval)

			result, err := u.ecsClient.DescribeServices(context.Background(), input)
			if err != nil {
				return fmt.Errorf("error describing services: %s", err)
			}

			for _, s := range result.Services {
				if s.DesiredCount == 0 {
					// mark this service deployment as complete
					serviceDeploymentComplete[*s.ServiceArn] = true
					continue
				}
				primaryComplete, stillHasActive := false, false
				for _, d := range s.Deployments {
					if *d.Status == "PRIMARY" && d.DesiredCount == d.RunningCount {
						primaryComplete = true
					}
					if *d.Status == "ACTIVE" && d.RunningCount > 0 {
						stillHasActive = true
						break
					}
				}

				// If the primary deployment is not yet complete, or the service still has an active
				// deployment draining tasks, continue waiting
				if !primaryComplete || stillHasActive {
					u.logger.Printf("Service %s still has incomplete deployments\n", *s.ServiceName)
					serviceDeploymentComplete[*s.ServiceArn] = false
					continue loop
				}

				// mark this service deployment as complete
				serviceDeploymentComplete[*s.ServiceArn] = true
			}

			for arn, isComplete := range serviceDeploymentComplete {
				if !isComplete {
					u.logger.Printf("Service %s still shows incomplete deployment", arn)
					continue loop
				}
			}

			break
		}
	}

	return nil
}

func (u *Upgrader) listServiceARNs() ([]string, error) {
	input := &ecs.ListServicesInput{
		Cluster:    aws.String(u.cluster),
		MaxResults: aws.Int32(100),
	}

	var services []string
	paginator := ecs.NewListServicesPaginator(u.ecsClient, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return []string{}, fmt.Errorf("error getting page of services: %s", err)
		}
		services = append(services, page.ServiceArns...)
	}

	return services, nil
}

func (u *Upgrader) terminateInstances(instances []string) error {
	if len(instances) == 0 {
		return fmt.Errorf("must include at least one instance ID for termination")
	}

	u.logger.Printf("Terminating instances: %s\n", strings.Join(instances, ", "))
	_, err := u.ec2Client.TerminateInstances(context.Background(), &ec2.TerminateInstancesInput{
		InstanceIds: instances,
	})

	return err
}

func (u *Upgrader) terminateOrphanedInstances(asgName string) error {
	orphans, err := u.findDetachedButRunningInstances(asgName)
	if err != nil {
		return fmt.Errorf("failed to terminate orphaned instances: %s", err)
	}
	if len(orphans) == 0 {
		u.logger.Printf("No orphaned instances found for ASG %s\n", asgName)
		return nil
	}

	u.logger.Printf("Found orphaned instances: %s\n", strings.Join(orphans, ", "))
	u.logger.Printf("Will terminate one at a time and wait for steady state\n")
	for _, id := range orphans {
		if err := u.safeTerminateInstance(id); err != nil {
			return err
		}
	}

	u.logger.Println("Orphaned instances terminated")
	return nil
}

// findDetachedByRunningInstances searches through all non-terminated EC2 instances for any
// that were previously attached to the given ASG that should be terminated. This enables
// ecs-ami-deploy to pick up where it left off due to premature exit (or timeout)
func (u *Upgrader) findDetachedButRunningInstances(asgName string) ([]string, error) {
	ec2Paginator := ec2.NewDescribeInstancesPaginator(u.ec2Client, &ec2.DescribeInstancesInput{
		MaxResults: aws.Int32(100),
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:" + TagNameASG),
				Values: []string{asgName},
			},
			{
				Name:   aws.String("tag:" + TagNameASG),
				Values: []string{asgName},
			},
		},
	})

	var orphanInstances []string
	for ec2Paginator.HasMorePages() {
		page, err := ec2Paginator.NextPage(context.Background())
		if err != nil {
			return []string{}, fmt.Errorf("error getting next page of detached instances: %s", err)
		}
		for _, r := range page.Reservations {
			for _, i := range r.Instances {
				// only look at instances that are not terminated
				if i.State.Name == ec2types.InstanceStateNameTerminated {
					continue
				}

				// double check presence of necessary tags for termination, cause why not?
				hasAsgTag, hasTerminateTag := false, false
				for _, t := range i.Tags {
					if *t.Key == TagNameASG && *t.Value == asgName {
						hasAsgTag = true
					}
					if *t.Key == TagNameTerminate && *t.Value == "true" {
						hasTerminateTag = true
					}
				}
				if hasAsgTag && hasTerminateTag {
					orphanInstances = append(orphanInstances, *i.InstanceId)
				}
			}
		}
	}

	return orphanInstances, nil
}

func (u *Upgrader) cleanupOldLaunchTemplates() error {
	input := &ec2.DescribeLaunchTemplatesInput{
		MaxResults: aws.Int32(100),
	}

	var relevantTemplates []ec2types.LaunchTemplate
	paginator := ec2.NewDescribeLaunchTemplatesPaginator(u.ec2Client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return fmt.Errorf("error retrieving page of launch templates: %s", err)
		}
		for _, lt := range page.LaunchTemplates {
			if strings.HasPrefix(*lt.LaunchTemplateName, u.launchTemplateNamePrefix) {
				relevantTemplates = append(relevantTemplates, lt)
			}
		}
	}

	if len(relevantTemplates) == 0 || len(relevantTemplates) <= u.launchTemplateLimit {
		return nil
	}
	u.logger.Printf("Found %v launch templates with prefix %s. Configured to only keep %v, will delete oldest revisions",
		len(relevantTemplates), u.launchTemplateNamePrefix, u.launchTemplateLimit)

	versions, err := u.getTemplateVersions(relevantTemplates)
	if err != nil {
		return err
	}

	// sort launch template versions newest to oldest
	reverseSortLaunchTemplateVersions(versions)

	for i := u.launchTemplateLimit; i < len(versions); i++ {
		versionString := fmt.Sprintf("%d", *versions[i].VersionNumber)
		if err := u.deleteLaunchTemplateVersion(*versions[i].LaunchTemplateName, versionString); err != nil {
			return fmt.Errorf("error deleting launch template %s version %d: %w",
				*versions[i].LaunchTemplateName, *versions[i].VersionNumber, err)
		}
	}

	return nil
}

func (u *Upgrader) deleteLaunchTemplateVersion(templateName, version string) error {
	input := &ec2.DeleteLaunchTemplateVersionsInput{
		LaunchTemplateName: aws.String(templateName),
		Versions:           []string{version},
	}

	u.logger.Printf("Deleting launch template %s version %s", templateName, version)

	_, err := u.ec2Client.DeleteLaunchTemplateVersions(context.Background(), input)
	return err
}

// isNewerImage checks if the second image is newer than the first
func isNewerImage(first, second ec2types.Image) (bool, error) {
	// creationDateFormat = 2019-03-04T19:15:04.000Z

	firstTime, err := time.Parse(time.RFC3339, *first.CreationDate)
	if err != nil {
		return false, err
	}

	secondTime, err := time.Parse(time.RFC3339, *second.CreationDate)
	if err != nil {
		return false, err
	}

	// is second time after first time?
	return secondTime.After(firstTime), nil
}

func reverseSortLaunchTemplateVersions(ltv []ec2types.LaunchTemplateVersion) {
	sort.SliceStable(ltv, func(i, j int) bool {
		return ltv[i].CreateTime.UnixNano() > ltv[j].CreateTime.UnixNano()
	})
}

func (u *Upgrader) getTemplateVersions(templates []ec2types.LaunchTemplate) (versions []ec2types.LaunchTemplateVersion, err error) {
	for _, t := range templates {
		in := ec2.DescribeLaunchTemplateVersionsInput{
			LaunchTemplateId: t.LaunchTemplateId,
		}
		v, err := u.ec2Client.DescribeLaunchTemplateVersions(context.Background(), &in)
		if err != nil {
			return nil, err
		}
		versions = append(versions, v.LaunchTemplateVersions...)
	}
	return
}
