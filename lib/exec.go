package lib

import (
    "fmt"
    "github.com/aws/aws-sdk-go/aws"
    //"github.com/aws/aws-sdk-go/aws/session"
    "github.com/aws/aws-sdk-go/service/ecs"
    "github.com/aws/aws-sdk-go/service/ecs/ecsiface"
)

// FindLatestTaskArn finds the latest task ARN for the specified service within a cluster
func FindLatestTaskArn(svc ecsiface.ECSAPI, clusterName, serviceName string) (string, error) {
    input := &ecs.ListTasksInput{
        Cluster:       aws.String(clusterName),
        ServiceName:   aws.String(serviceName),
        DesiredStatus: aws.String("RUNNING"),
        MaxResults:    aws.Int64(1),
    }

    result, err := svc.ListTasks(input)
    if err != nil || len(result.TaskArns) == 0 {
        return "", fmt.Errorf("no running tasks found for service %s on cluster %s", serviceName, clusterName)
    }

    return aws.StringValue(result.TaskArns[0]), nil
}

// ExecuteCommandInContainer executes a specified command in a running container on an ECS Fargate cluster.
func ExecuteCommandInContainer(svc ecsiface.ECSAPI, cluster, serviceName, containerName, command string) error {
    taskArn, err := FindLatestTaskArn(svc, cluster, serviceName)
    if err != nil {
        return err
    }

    input := &ecs.ExecuteCommandInput{
        Cluster:     aws.String(cluster),
        Task:        aws.String(taskArn),
        Container:   aws.String(containerName),
        Interactive: aws.Bool(true),
        Command:     aws.String(command),
    }

    _, err = svc.ExecuteCommand(input)
    if err != nil {
        return fmt.Errorf("failed to execute command: %v", err)
    }

    return nil
}
