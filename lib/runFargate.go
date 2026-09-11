package lib

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// RunFargate runs the specified one-off task in the cluster using the task definition
func RunFargate(profile, cluster, service, taskDefinitionName, imageTag string, imageTags []string, workDir, containerName, awslogGroup, launchType string, securityGroupFilter string, args []string) (exitCode int, err error) {
	err = InitAWS(profile)
	if err != nil {
		return 1, err
	}
	ctx := log.WithFields(log.Fields{"task_definition": taskDefinitionName})

	svc := sessionInstance
	svcEC2 := ec2.NewFromConfig(sessionConfig)

	// Fetch subnets and security groups
	subnets, err := fetchSubnetsByTag(svcEC2, "Tier", "private")
	if err != nil {
		log.WithError(err).Error("Failed to fetch subnets by  private tag")
		return 1, err
	}
	if len(subnets) == 0 {
		subnets, err = fetchSubnetsByTag(svcEC2, "Tier", "public")

		if err != nil {
			log.WithError(err).Error("Failed to fetch subnets by public tag")
			return 1, err
		}
	}
	securityGroups, err := fetchSecurityGroupsByName(svcEC2, securityGroupFilter)
	if err != nil {
		log.WithError(err).Error("Failed to fetch security groups by name")
		return 1, err
	}
	// Set up network configuration
	networkConfiguration := &types.NetworkConfiguration{
		AwsvpcConfiguration: &types.AwsVpcConfiguration{
			Subnets:        subnets,
			SecurityGroups: securityGroups,
			// Currently we always use public IPs for Fargate tasks to ensure internet access.
			// This will be changed when IPv6 support is implemented, as IPv6 provides global
			// addressing and may eliminate the need for public IPs depending on subnet configuration.
			AssignPublicIp: types.AssignPublicIpEnabled,
		},
	}

	ctx.WithFields(log.Fields{
		"Cluster":        cluster,
		"TaskDefinition": taskDefinitionName,
		"LaunchType":     launchType,
		"Subnets":        fmt.Sprint(subnets),
		"SecurityGroups": fmt.Sprint(securityGroups),
		"AssignPublicIP": string(networkConfiguration.AwsvpcConfiguration.AssignPublicIp),
	}).Info("Attempting to launch task")

	describeResult, err := svc.DescribeTaskDefinition(context.TODO(), &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(taskDefinitionName),
		Include:        []types.TaskDefinitionField{types.TaskDefinitionFieldTags},
	})
	if err != nil {
		ctx.WithError(err).Error("Can't get task definition")
		return 1, err
	}
	taskDefinition := describeResult.TaskDefinition

	var foundContainerName bool
	if err := modifyContainerDefinitionImages(imageTag, imageTags, workDir, taskDefinition.ContainerDefinitions, ctx); err != nil {
		return 1, err
	}
	for n, containerDefinition := range taskDefinition.ContainerDefinitions {
		if aws.ToString(containerDefinition.Name) == containerName {
			foundContainerName = true
			// Use shell execution to interpret the command with any arguments
			commandLine := strings.Join(args, " ") // Join args into a single command line
			containerDefinition.Command = []string{"sh", "-c", commandLine}
			if awslogGroup != "" {
				containerDefinition.LogConfiguration = &types.LogConfiguration{
					LogDriver: types.LogDriverAwslogs,
					Options: map[string]string{
						"awslogs-region":        sessionConfig.Region,
						"awslogs-group":         awslogGroup,
						"awslogs-stream-prefix": cluster,
					},
				}
			}
			taskDefinition.ContainerDefinitions[n] = containerDefinition // Update the container definition

		}
	}
	if !foundContainerName {
		err := fmt.Errorf("can't find container with specified name in the task definition")
		ctx.WithFields(log.Fields{"container_name": containerName}).Error(err.Error())
		return 1, err
	}

	registerResult, err := svc.RegisterTaskDefinition(context.TODO(), &ecs.RegisterTaskDefinitionInput{
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
		Tags:                    nilIfEmpty(describeResult.Tags),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't register task definition")
		return 1, err
	}
	ctx.WithField("task_definition_arn", aws.ToString(registerResult.TaskDefinition.TaskDefinitionArn)).Debug("Registered the task definition")

	// Deregister the task definition
	defer func() {
		_, err = svc.DeregisterTaskDefinition(context.TODO(), &ecs.DeregisterTaskDefinitionInput{
			TaskDefinition: registerResult.TaskDefinition.TaskDefinitionArn,
		})
		if err != nil {
			ctx.WithError(err).Error("Can't deregister task definition")
		}
	}()

	// Run the task with network configuration
	runTaskInput := ecs.RunTaskInput{
		Cluster:              aws.String(cluster),
		TaskDefinition:       registerResult.TaskDefinition.TaskDefinitionArn,
		Count:                aws.Int32(1),
		StartedBy:            aws.String("go-deploy"),
		LaunchType:           types.LaunchType(launchType),
		NetworkConfiguration: networkConfiguration,
	}

	runResult, err := svc.RunTask(context.TODO(), &runTaskInput)
	if err != nil {
		ctx.WithError(err).Error("Can't run specified task")
		return 1, err
	}
	if len(runResult.Tasks) == 0 {
		ctx.Error("No tasks could be run. Please check if the ECS cluster has enough resources")
		return 1, err
	}

	ctx.Info("Waiting for the task to finish")
	var tasks []string
	for _, task := range runResult.Tasks {
		tasks = append(tasks, aws.ToString(task.TaskArn))
		ctx.WithField("task_arn", aws.ToString(task.TaskArn)).Debug("Started task")
	}
	tasksInput := &ecs.DescribeTasksInput{
		Cluster: aws.String(cluster),
		Tasks:   tasks,
	}
	waiter := ecs.NewTasksStoppedWaiter(svc)
	err = waiter.Wait(context.TODO(), tasksInput, 10*time.Minute)
	if err != nil {
		ctx.WithError(err).Error("The waiter has been finished with an error")
		exitCode = 3
		return exitCode, err
	}

	tasksOutput, err := svc.DescribeTasks(context.TODO(), tasksInput)
	if err != nil {
		ctx.WithError(err).Error("Can't describe stopped tasks")
		return 1, err
	}

	for _, task := range tasksOutput.Tasks {
		for _, container := range task.Containers {
			ctx := log.WithFields(log.Fields{
				"container_name": aws.ToString(container.Name),
			})
			reason := aws.ToString(container.Reason)
			if len(reason) != 0 {
				exitCode = 11
				ctx = ctx.WithField("reason", reason)
			} else {
				ctx = ctx.WithField("exit_code", aws.ToInt32(container.ExitCode))

			}
			if aws.ToInt32(container.ExitCode) == 0 && len(reason) == 0 {
				ctx.Info("Container exited")
			} else {
				ctx.Error("Container exited")
			}

			if aws.ToString(container.Name) == containerName {
				if len(reason) == 0 {
					exitCode = int(aws.ToInt32(container.ExitCode))

					if awslogGroup != "" {
						// get log output
						taskUUID, err := parseTaskUUID(container.TaskArn)
						if err != nil {
							log.WithFields(log.Fields{"task_arn": aws.ToString(container.TaskArn)}).WithError(err).Error("Can't parse task uuid")
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

	return exitCode, nil
}
