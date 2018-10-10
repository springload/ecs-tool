package deploy

import (
	"fmt"
	"strings"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
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
			return fmt.Errorf("Can't get aws session.")
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
	if len(split) != 2 {
		return "", fmt.Errorf("Weird task arn, can't get resource UUID")
	}
	return split[1], nil
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
