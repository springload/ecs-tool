package deploy

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
)

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
