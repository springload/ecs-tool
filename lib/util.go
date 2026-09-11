package lib

import (
	"context"
	"fmt"
	"strings"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

func parseTaskUUID(containerArn *string) (string, error) {
	resourceArn, err := arn.Parse(aws.ToString(containerArn))
	if err != nil {
		return "", err
	}
	split := strings.Split(resourceArn.Resource, "/")
	switch len(split) {
	case 2:
		return split[1], nil
	case 3:
		return split[2], nil
	}

	return "", fmt.Errorf("weird task arn, can't get resource UUID")
}

func printCloudWatchLogs(logGroup, streamName string) error {
	logs := cloudwatchlogs.NewFromConfig(sessionConfig)
	paginator := cloudwatchlogs.NewGetLogEventsPaginator(logs, &cloudwatchlogs.GetLogEventsInput{
		LogGroupName: aws.String(logGroup),
		// prefix-name/container-name/ecs-task-id
		LogStreamName: aws.String(streamName),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			return err
		}
		for _, event := range page.Events {
			fmt.Println(aws.ToString(event.Message))
		}
	}
	return nil
}

func deleteCloudWatchStream(logGroup, streamName string) error {
	logs := cloudwatchlogs.NewFromConfig(sessionConfig)
	_, err := logs.DeleteLogStream(context.TODO(), &cloudwatchlogs.DeleteLogStreamInput{
		LogGroupName:  aws.String(logGroup),
		LogStreamName: aws.String(streamName),
	})

	return err
}

func fetchCloudWatchLog(cluster, containerName, awslogGroup, taskUUID string, delete bool, ctx *log.Entry) error {
	streamName := strings.Join([]string{cluster, containerName, taskUUID}, "/")

	defer func() {
		ctx := ctx.WithFields(log.Fields{
			"log_group":  awslogGroup,
			"log_stream": streamName,
		})
		if err := deleteCloudWatchStream(awslogGroup, streamName); err != nil {
			ctx.WithError(err).Error("Can't delete the log stream")
		} else {
			ctx.Debug("Deleted log stream")
		}
	}()
	return printCloudWatchLogs(awslogGroup, streamName)
}

func modifyContainerDefinitionImages(imageTag string, imageTags []string, workDir string, containerDefinitions []types.ContainerDefinition, ctx log.Interface) error {

	for n, containerDefinition := range containerDefinitions {
		ctx := ctx.WithField("container_name", aws.ToString(containerDefinition.Name))
		imageWithTag := strings.SplitN(aws.ToString(containerDefinition.Image), ":", 2)

		if len(imageWithTag) == 2 { // successfully split into 2 parts: repo and tag
			var newTag string // if set we'll change the definition
			if imageTag != "" {
				newTag = imageTag // this takes precedence
			} else if len(imageTags) > n && imageTags[n] != "" { // the expression below will make this obsolete, as if the tag is "", then it won't be used anyway. But just adding this condition here to be explicit, just in case we want to do something in here later.
				newTag = imageTags[n]
			}

			if newTag != "" {
				// replace some [arams
				newTag = strings.ReplaceAll(newTag, "{container_name}", aws.ToString(containerDefinition.Name))
				image := strings.Join([]string{
					imageWithTag[0],
					newTag,
				}, ":")
				containerDefinitions[n].Image = aws.String(image)
				ctx.WithFields(log.Fields{
					"image":   image,
					"new_tag": newTag,
					"old_tag": imageWithTag[1],
				}).Debug("Image tag changed")
			}
		} else {
			ctx.Debug("Container doesn't seem to have a tag in the image. It's safer to not do anything.")
		}
		if workDir != "" {
			containerDefinitions[n].WorkingDirectory = aws.String(workDir)
			ctx.WithField("workdir", workDir).Debug("Workdir changed")
		}

	}
	return nil
}

// fetchSubnetsByTag fetches subnet IDs by a specific tag name and value
func fetchSubnetsByTag(svc *ec2.Client, tagKey, tagValue string) ([]string, error) {
	input := &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String(fmt.Sprintf("tag:%s", tagKey)),
				Values: []string{tagValue},
			},
		},
	}

	result, err := svc.DescribeSubnets(context.TODO(), input)
	if err != nil {
		return nil, fmt.Errorf("error describing subnets: %w", err)
	}

	var subnets []string
	for _, subnet := range result.Subnets {
		subnets = append(subnets, aws.ToString(subnet.SubnetId))
	}

	return subnets, nil
}

func fetchSecurityGroupsByName(svc *ec2.Client, securityGroupFilter string) ([]string, error) {
	// Describe all security groups
	input := &ec2.DescribeSecurityGroupsInput{}

	result, err := svc.DescribeSecurityGroups(context.TODO(), input)
	if err != nil {
		return nil, fmt.Errorf("error describing security groups: %w", err)
	}

	var securityGroups []string
	// Loop through the security groups and add those that contain the filter in their name
	for _, sg := range result.SecurityGroups {
		if strings.Contains(aws.ToString(sg.GroupName), securityGroupFilter) {
			securityGroups = append(securityGroups, aws.ToString(sg.GroupId))
		}
	}

	// Return the filtered list of security group IDs
	return securityGroups, nil
}

// nilIfEmpty returns nil when tags is empty so AWS doesn't reject the call with
// "Tags can not be empty" — the AWS API rejects an empty Tags list at the wire level;
// passing nil omits the field entirely.
func nilIfEmpty(tags []types.Tag) []types.Tag {
	if len(tags) == 0 {
		return nil
	}
	return tags
}
