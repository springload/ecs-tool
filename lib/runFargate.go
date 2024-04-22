package lib

import (
    "fmt"
    "github.com/apex/log"
    "github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/service/ecs"
    "github.com/aws/aws-sdk-go/service/ec2"
	"strings"
)

// RunFargate runs the specified one-off task in the cluster using the task definition
func RunFargate(profile, cluster, service, taskDefinitionName, imageTag string, imageTags []string, workDir, containerName, awslogGroup, launchType string, securityGroupFilter string, args []string) (exitCode int, err error) {
    err = makeSession(profile)
    if err != nil {
        return 1, err
    }
    ctx := log.WithFields(log.Fields{"task_definition": taskDefinitionName})


    svc := ecs.New(localSession)
    svcEC2 := ec2.New(localSession)  // Assuming makeSession initializes localSession

    // Fetch subnets and security groups
    subnets, err := fetchSubnetsByTag(svcEC2, "Tier", "private")
    if err != nil {
        log.WithError(err).Error("Failed to fetch subnets by tag")
        return 1, err
    }
securityGroups, err := fetchSecurityGroupsByName(svcEC2, securityGroupFilter)
	if err != nil {
        log.WithError(err).Error("Failed to fetch security groups by name")
        return 1, err
    }
    // Set up network configuration
    networkConfiguration := &ecs.NetworkConfiguration{
        AwsvpcConfiguration: &ecs.AwsVpcConfiguration{
            Subnets:        subnets,
            SecurityGroups: securityGroups,
            AssignPublicIp: aws.String("ENABLED"), // or "ENABLED" if public IP is needed
        },
    }


	ctx.WithFields(log.Fields{
    "Cluster":        aws.StringValue(aws.String(cluster)),
    "TaskDefinition": aws.StringValue(aws.String(taskDefinitionName)),
    "LaunchType":     aws.StringValue(aws.String(launchType)),
    "Subnets":        fmt.Sprint(subnets),
    "SecurityGroups": fmt.Sprint(securityGroups),
    "AssignPublicIP": aws.StringValue(networkConfiguration.AwsvpcConfiguration.AssignPublicIp),
}).Info("Attempting to launch task")

    describeResult, err := svc.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{
        TaskDefinition: aws.String(taskDefinitionName),
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
    if aws.StringValue(containerDefinition.Name) == containerName {
        foundContainerName = true
        // Use shell execution to interpret the command with any arguments
        commandLine := strings.Join(args, " ") // Join args into a single command line
        containerDefinition.Command = []*string{aws.String("sh"), aws.String("-c"), aws.String(commandLine)}
        if awslogGroup != "" {
            containerDefinition.LogConfiguration = &ecs.LogConfiguration{
                LogDriver: aws.String("awslogs"),
                Options: map[string]*string{
                    "awslogs-region":        localSession.Config.Region,
                    "awslogs-group":         aws.String(awslogGroup),
                    "awslogs-stream-prefix": aws.String(cluster),
                },
            }
        }
        taskDefinition.ContainerDefinitions[n] = containerDefinition // Update the container definition
		
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
    ctx.WithField("task_definition_arn", aws.StringValue(registerResult.TaskDefinition.TaskDefinitionArn)).Debug("Registered the task definition")

    // Deregister the task definition
    defer func() {
        _, err = svc.DeregisterTaskDefinition(&ecs.DeregisterTaskDefinitionInput{
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
        Count:                aws.Int64(1),
        StartedBy:            aws.String("go-deploy"),
        LaunchType:           aws.String(launchType),
        NetworkConfiguration: networkConfiguration,
    }

    runResult, err := svc.RunTask(&runTaskInput)
    if err != nil {
        ctx.WithError(err).Error("Can't run specified task")
        return 1, err
    }
    if len(runResult.Tasks) == 0 {
        ctx.Error("No tasks could be run. Please check if the ECS cluster has enough resources")
        return 1, err
    }

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
        exitCode = 3
        return exitCode, err
    }

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

    return exitCode, nil
}



