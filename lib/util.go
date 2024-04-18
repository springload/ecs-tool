package lib

import (
	"fmt"
	"strings"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/ecs"
    "github.com/aws/aws-sdk-go/service/ec2"
)

var localSession *session.Session

func makeSession(profile string) error {
	if localSession == nil {
		log.Debug("Creating session")
		var err error
		// create AWS session
		localSession, err = session.NewSessionWithOptions(session.Options{
			Config: aws.Config{},

			SharedConfigState: session.SharedConfigEnable,
			Profile:           profile,
		})
		if err != nil {
			return fmt.Errorf("can't get aws session")
		}
	}
	return nil
}

func parseTaskUUID(containerArn *string) (string, error) {
	resourceArn, err := arn.Parse(aws.StringValue(containerArn))
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

	return "", fmt.Errorf("Weird task arn, can't get resource UUID")
}

func printCloudWatchLogs(logGroup, streamName string) error {
	logs := cloudwatchlogs.New(localSession)
	err := logs.GetLogEventsPages(
		&cloudwatchlogs.GetLogEventsInput{
			LogGroupName: aws.String(logGroup),
			// prefix-name/container-name/ecs-task-id
			LogStreamName: aws.String(streamName),
		},
		func(page *cloudwatchlogs.GetLogEventsOutput, lastPage bool) bool {
			if len(page.Events) > 0 {
				for _, event := range page.Events {
					fmt.Println(aws.StringValue(event.Message))
				}
			}
			return true
		})
	return err

}
func deleteCloudWatchStream(logGroup, streamName string) error {
	logs := cloudwatchlogs.New(localSession)
	_, err := logs.DeleteLogStream(&cloudwatchlogs.DeleteLogStreamInput{
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

func modifyContainerDefinitionImages(imageTag string, imageTags []string, workDir string, containerDefinitions []*ecs.ContainerDefinition, ctx log.Interface) error {

	for n, containerDefinition := range containerDefinitions {
		ctx := ctx.WithField("container_name", aws.StringValue(containerDefinition.Name))
		imageWithTag := strings.SplitN(aws.StringValue(containerDefinition.Image), ":", 2)

		if len(imageWithTag) == 2 { // successfully split into 2 parts: repo and tag
			var newTag string // if set we'll change the definition
			if imageTag != "" {
				newTag = imageTag // this takes precedence
			} else if len(imageTags) > n && imageTags[n] != "" { // the expression below will make this obsolete, as if the tag is "", then it won't be used anyway. But just adding this condition here to be explicit, just in case we want to do something in here later.
				newTag = imageTags[n]
			}

			if newTag != "" {
				// replace some [arams
				newTag = strings.Replace(newTag, "{container_name}", aws.StringValue(containerDefinition.Name), -1)
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
func fetchSubnetsByTag(svc *ec2.EC2, tagKey, tagValue string) ([]*string, error) {
    input := &ec2.DescribeSubnetsInput{
        Filters: []*ec2.Filter{
            {
                Name:   aws.String(fmt.Sprintf("tag:%s", tagKey)),
                Values: []*string{aws.String(tagValue)},
            },
        },
    }

    result, err := svc.DescribeSubnets(input)
    if err != nil {
        return nil, fmt.Errorf("error describing subnets: %w", err)
    }

    var subnets []*string
    for _, subnet := range result.Subnets {
        subnets = append(subnets, subnet.SubnetId)
    }

    return subnets, nil
}


func fetchSecurityGroupsByName(svc *ec2.EC2, securityGroupFilter string) ([]*string, error) {
    // Describe all security groups
    input := &ec2.DescribeSecurityGroupsInput{}

    result, err := svc.DescribeSecurityGroups(input)
    if err != nil {
        return nil, fmt.Errorf("error describing security groups: %w", err)
    }

    var securityGroups []*string
    // Loop through the security groups and add those that contain the filter in their name
    for _, sg := range result.SecurityGroups {
        if strings.Contains(*sg.GroupName, securityGroupFilter) {
            securityGroups = append(securityGroups, sg.GroupId)
        }
    }

    // Return the filtered list of security group IDs
    return securityGroups, nil
}

