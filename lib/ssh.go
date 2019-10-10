package lib

import (
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2instanceconnect"
	"github.com/aws/aws-sdk-go/service/ecs"
	"golang.org/x/crypto/ssh/agent"
)

// ConnectSSH runs ssh with some magic parameters to connect to running containers on AWS ECS
func ConnectSSH(profile, cluster, taskDefinitionName, containerName, shell, service, instanceUser string) (exitCode int, err error) {
	err = makeSession(profile)
	if err != nil {
		return 1, err
	}
	ctx := log.WithFields(&log.Fields{"task_definition": taskDefinitionName})

	svc := ecs.New(localSession)

	ctx.Info("Looking for ECS Task...")

	listResult, err := svc.ListTasks(&ecs.ListTasksInput{
		Cluster:     aws.String(cluster),
		ServiceName: aws.String(service),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't get task list")
		return 1, err
	}

	describeResult, err := svc.DescribeTasks(&ecs.DescribeTasksInput{
		Cluster: aws.String(cluster),
		Tasks:   listResult.TaskArns,
	})
	if err != nil {
		ctx.WithError(err).Error("Can't describe tasks")
		return 1, err
	}

	tasks := describeResult.Tasks
	var foundTask *ecs.Task
	for _, task := range tasks {
		if strings.Contains(aws.StringValue(task.TaskDefinitionArn), taskDefinitionName) {
			foundTask = task
		}
	}

	if foundTask == nil {
		err := fmt.Errorf("Can't find matching task")
		ctx.WithFields(log.Fields{"task_definition": taskDefinitionName}).Error(err.Error())
		return 1, err
	}

	ctx.WithField("task_arn", aws.StringValue(foundTask.TaskArn)).Info("Looking for EC2 Instance...")

	contInstanceResult, err := svc.DescribeContainerInstances(&ecs.DescribeContainerInstancesInput{
		ContainerInstances: []*string{foundTask.ContainerInstanceArn},
		Cluster:            aws.String(cluster),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't get container instance")
		return 1, err
	}

	instance := contInstanceResult.ContainerInstances[0]
	instanceID := instance.Ec2InstanceId

	ec2Svc := ec2.New(localSession)
	ec2Result, err := ec2Svc.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{instanceID},
	})
	if err != nil {
		ctx.WithError(err).Error("Can't get ec2 instance")
		return 1, err
	}

	ec2Instance := ec2Result.Reservations[0].Instances[0]
	ec2ICSvc := ec2instanceconnect.New(localSession)

	ctx.WithField("instance_id", aws.StringValue(ec2Instance.InstanceId)).Info("Pushing SSH key...")

	sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))

	keys, err := agent.NewClient(sshAgent).List()
	if err != nil || len(keys) < 1 {
		ctx.WithError(err).Error("Can't get public keys from ssh agent. Please ensure you have the ssh-agent running and have at least one identity added (with ssh-add)")
		return 1, err
	}
	pubkey := keys[0].String()

	_, err = ec2ICSvc.SendSSHPublicKey(&ec2instanceconnect.SendSSHPublicKeyInput{
		InstanceId:       ec2Instance.InstanceId,
		InstanceOSUser:   aws.String(instanceUser),
		AvailabilityZone: ec2Instance.Placement.AvailabilityZone,
		SSHPublicKey:     aws.String(pubkey),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't push SSH key")
		return 1, err
	}

	ctx.WithField("instance_id", aws.StringValue(ec2Instance.InstanceId)).Info("Connecting to container...")

	params := []string{
		"ssh",
		"-tt",
		fmt.Sprintf("%s@%s.%s", instanceUser, aws.StringValue(ec2Instance.PrivateIpAddress), profile),
		"docker-exec",
		aws.StringValue(foundTask.TaskArn),
		containerName,
		shell,
	}

	env := os.Environ()

	syscall.Exec("/usr/bin/ssh", params, env)

	return 0, nil
}
