package deploy

import (
	"fmt"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
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

func RunTask(profile, cluster, taskDefinitionName, containerName, awslogGroup string, args []string) (exitCode int, err error) {
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
			break
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

	ctx.Info("waiting for the task to finish")
	var tasks []*string
	for _, task := range runResult.Tasks {
		tasks = append(tasks, task.TaskArn)
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
		ctx = ctx.WithFields(log.Fields{"temp_task_definition": aws.StringValue(registerResult.TaskDefinition.TaskDefinitionArn)})
		ctx.Debug("Deregistering task definition")
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
	for _, container := range tasksOutput.Tasks[0].Containers {
		ctx := log.WithFields(log.Fields{
			"container_name": aws.StringValue(container.Name),
			"exit_code":      aws.Int64Value(container.ExitCode),
		})
		if aws.Int64Value(container.ExitCode) == 0 {
			ctx.Info("container exited")
		} else {
			ctx.Error("container exited")
		}
		if aws.StringValue(container.Name) == containerName {
			exitCode = int(aws.Int64Value(container.ExitCode))
			if awslogGroup != "" {
				// get log output
				taskUUID, err := parseTaskUUID(container.TaskArn)
				if err != nil {
					log.WithFields(log.Fields{"task_arn": container.TaskArn}).WithError(err).Error("Can't parse task uuid")
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

	return

}
