package lib

import (
	"fmt"
	"strings"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecs"
)

// RunTask runs the specified one-off task in the cluster using the task definition
func RunTask(profile, cluster, taskDefinitionName, imageTag, containerName, awslogGroup string, args []string) (exitCode int, err error) {
	err = makeSession(profile)
	if err != nil {
		return 1, err
	}
	ctx := log.WithFields(&log.Fields{"task_definition": taskDefinitionName})

	svc := ecs.New(localSession)

	describeResult, err := svc.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(taskDefinitionName),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't get task definition")
		return 1, err
	}
	taskDefinition := describeResult.TaskDefinition
	//var containerNumber int
	var foundContainerName bool
	for n, containerDefinition := range taskDefinition.ContainerDefinitions {
		if imageTag != "" {
			imageWithTag := strings.SplitN(aws.StringValue(containerDefinition.Image), ":", 2)
			if len(imageWithTag) == 2 { // successfully split into 2 parts: repo and tag
				image := strings.Join([]string{
					imageWithTag[0],
					imageTag,
				}, ":")
				taskDefinition.ContainerDefinitions[n].Image = aws.String(image)
				ctx.WithFields(log.Fields{
					"container_name": aws.StringValue(containerDefinition.Name),
					"image":          image,
				}).Debug("Image tag changed")
			}
		}
		if aws.StringValue(containerDefinition.Name) == containerName {
			foundContainerName = true
			taskDefinition.ContainerDefinitions[n].Command = aws.StringSlice(args)
			if awslogGroup != "" {
				// modify log output driver to capture output to a predefined CloudWatch log
				taskDefinition.ContainerDefinitions[n].LogConfiguration = &ecs.LogConfiguration{
					LogDriver: aws.String("awslogs"),
					Options: map[string]*string{
						"awslogs-region":        localSession.Config.Region,
						"awslogs-group":         aws.String(awslogGroup),
						"awslogs-stream-prefix": aws.String(cluster),
					},
				}
			}
		}
	}
	if !foundContainerName {
		err := fmt.Errorf("Can't find container with specified name in the task definition")
		ctx.WithFields(log.Fields{"container_name": containerName}).Error(err.Error())
		return 1, err
	}
	registerResult, err := svc.RegisterTaskDefinition(&ecs.RegisterTaskDefinitionInput{
		ContainerDefinitions:    taskDefinition.ContainerDefinitions,
		Cpu:                     taskDefinition.Cpu,
		ExecutionRoleArn:        taskDefinition.ExecutionRoleArn,
		Family:                  taskDefinition.Family,
		Memory:                  taskDefinition.Memory,
		NetworkMode:             taskDefinition.NetworkMode,
		PlacementConstraints:    taskDefinition.PlacementConstraints,
		RequiresCompatibilities: taskDefinition.Compatibilities,
		TaskRoleArn:             taskDefinition.TaskRoleArn,
		Volumes:                 taskDefinition.Volumes,
	})
	if err != nil {
		ctx.WithError(err).Error("Can't register task definition")
		return 1, err
	}
	ctx.WithField(
		"task_definition_arn",
		aws.StringValue(registerResult.TaskDefinition.TaskDefinitionArn),
	).Debug("Registered the task definition")
	runResult, err := svc.RunTask(&ecs.RunTaskInput{
		Cluster:        aws.String(cluster),
		TaskDefinition: registerResult.TaskDefinition.TaskDefinitionArn,
		Count:          aws.Int64(1),
		StartedBy:      aws.String("go-deploy"),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't run specified task")
		return 1, err
	}
	// the task should be in PENDING state at this point

	ctx.Info("Waiting for the task to finish")
	var tasks []*string
	for _, task := range runResult.Tasks {
		tasks = append(tasks, task.TaskArn)
		ctx.WithField("task_arn", aws.StringValue(task.TaskArn)).Debug("Started task")
	}
	tasksInput := &ecs.DescribeTasksInput{
		Cluster: aws.String(cluster),
		Tasks:   tasks,
	}
	err = svc.WaitUntilTasksStopped(tasksInput)
	if err != nil {
		ctx.WithError(err).Error("The waiter has been finished with an error")
	}

	// deregister the definition
	defer func() {
		ctx = ctx.WithFields(log.Fields{"task_definition_arn": aws.StringValue(registerResult.TaskDefinition.TaskDefinitionArn)})
		ctx.Debug("Deregistered the task definition")
		_, err = svc.DeregisterTaskDefinition(&ecs.DeregisterTaskDefinitionInput{
			TaskDefinition: registerResult.TaskDefinition.TaskDefinitionArn,
		})
		if err != nil {
			ctx.WithError(err).Error("Can't deregister task definition")
		}
	}()
	tasksOutput, err := svc.DescribeTasks(tasksInput)
	if err != nil {
		ctx.WithError(err).Error("Can't describe stopped tasks")
		return 1, err
	}
	for _, task := range tasksOutput.Tasks {
		for _, container := range task.Containers {
			ctx := log.WithFields(log.Fields{
				"container_name": aws.StringValue(container.Name),
			})
			reason := aws.StringValue(container.Reason)
			if len(reason) != 0 {
				exitCode = 11
				ctx = ctx.WithField("reason", reason)
			} else {
				ctx = ctx.WithField("exit_code", aws.Int64Value(container.ExitCode))

			}
			if aws.Int64Value(container.ExitCode) == 0 && len(reason) == 0 {
				ctx.Info("Container exited")
			} else {
				ctx.Error("Container exited")
			}
			if aws.StringValue(container.Name) == containerName {
				if len(reason) == 0 {
					exitCode = int(aws.Int64Value(container.ExitCode))
					if awslogGroup != "" {
						// get log output
						taskUUID, err := parseTaskUUID(container.TaskArn)
						if err != nil {
							log.WithFields(log.Fields{"task_arn": aws.StringValue(container.TaskArn)}).WithError(err).Error("Can't parse task uuid")
							exitCode = 10
							continue
						}
						err = fetchCloudWatchLog(cluster, containerName, awslogGroup, taskUUID, false, ctx)
						if err != nil {
							log.WithError(err).Error("Can't fetch the logs")
							exitCode = 10
						}
					}
				}
			}
		}
	}

	return

}
