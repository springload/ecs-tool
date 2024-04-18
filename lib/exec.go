package lib

import (
    "fmt"
    "github.com/apex/log"
    "github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/session"
    "github.com/aws/aws-sdk-go/service/ecs"
    "github.com/aws/aws-sdk-go/service/ecs/ecsiface"
)

// InitializeSession creates an AWS session for ECS interaction
func InitializeSession(region, profile string) (*session.Session, error) {
    sess, err := session.NewSessionWithOptions(session.Options{
        Profile: profile,
        Config:  aws.Config{
        },
    })
    if err != nil {
        log.WithError(err).Error("Failed to create AWS session")
        return nil, err
    }
    return sess, nil
}

// FindLatestTaskArn locates the most recent task ARN for a specified ECS service
func FindLatestTaskArn(clusterName, serviceName string) (string, error) {
    if serviceName == "" {
        return "", fmt.Errorf("service name cannot be empty")
    }

    input := &ecs.ListTasksInput{
        Cluster:       aws.String(clusterName),
        ServiceName:   aws.String(serviceName),
        DesiredStatus: aws.String("RUNNING"),
        MaxResults:    aws.Int64(1),
    }

    result, err := svc.ListTasks(input)
    if err != nil {
        log.WithError(err).Error("Error listing tasks")
        return "", fmt.Errorf("error listing tasks for service %s on cluster %s: %v", serviceName, clusterName, err)
    }
    if len(result.TaskArns) == 0 {
        log.WithFields(log.Fields{
            "cluster": clusterName,
            "service": serviceName,
        }).Error("No running tasks found")
        return "", fmt.Errorf("no running tasks found for service %s on cluster %s", serviceName, clusterName)
    }

    log.WithFields(log.Fields{
        "taskArn": aws.StringValue(result.TaskArns[0]),
    }).Info("Found latest task ARN")
    return aws.StringValue(result.TaskArns[0]), nil
}

// ExecuteCommandInContainer runs a command in a specified container on an ECS Fargate service
func ExecFargate(rofile, cluster, service, container, workDir, containerName, awslogGroup, launchType string,   command string) (exitCode int, err error) {
    if service == "" {
        return fmt.Errorf("service name cannot be empty")
    }

    taskArn, err := FindLatestTaskArn(cluster, serviceName)
    if err != nil {
        return err
    }
    fmt.Println(taskArn , "we are here")
    input := &ecs.ExecuteCommandInput{
        Cluster:     aws.String(cluster),
        Task:        aws.String(taskArn),
        Container:   aws.String(containerName),
        Interactive: aws.Bool(true),
        Command:     aws.String(command),
    }

    _, err = svc.ExecuteCommand(input)
    if err != nil {
        log.WithError(err).WithFields(log.Fields{
            "taskArn":       taskArn,
            "containerName": containerName,
            "command":       command,
        }).Error("Failed to execute command")
        return fmt.Errorf("failed to execute command: %v", err)
    }

    log.WithFields(log.Fields{
        "taskArn":       taskArn,
        "containerName": containerName,
        "command":       command,
    }).Info("Command executed successfully")
    return nil
}
