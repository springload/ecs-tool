package lib

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2instanceconnect"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"golang.org/x/crypto/ssh/agent"
)

// ConnectSSH runs ssh with some magic parameters to connect to running containers on AWS ECS
func ConnectSSH(profile, cluster, taskDefinitionName, containerName, shell, service, instanceUser string, pushSSHKey bool) (exitCode int, err error) {
	err = InitAWS(profile)
	if err != nil {
		return 1, err
	}
	ctx := log.WithFields(&log.Fields{"task_definition": taskDefinitionName})

	svc := sessionInstance

	ctx.Info("Looking for ECS Task...")

	listResult, err := svc.ListTasks(context.TODO(), &ecs.ListTasksInput{
		Cluster:     aws.String(cluster),
		ServiceName: aws.String(service),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't get task list")
		return 1, err
	}

	describeResult, err := svc.DescribeTasks(context.TODO(), &ecs.DescribeTasksInput{
		Cluster: aws.String(cluster),
		Tasks:   listResult.TaskArns,
	})
	if err != nil {
		ctx.WithError(err).Error("Can't describe tasks")
		return 1, err
	}

	tasks := describeResult.Tasks
	var foundTask *types.Task
	for n, task := range tasks {
		if strings.Contains(aws.ToString(task.TaskDefinitionArn), taskDefinitionName) {
			foundTask = &tasks[n]
		}
	}

	if foundTask == nil {
		err := fmt.Errorf("can't find matching task")
		ctx.WithFields(log.Fields{"task_definition": taskDefinitionName}).Error(err.Error())
		return 1, err
	}

	ctx.WithField("task_arn", aws.ToString(foundTask.TaskArn)).Info("Looking for EC2 Instance...")

	contInstanceResult, err := svc.DescribeContainerInstances(context.TODO(), &ecs.DescribeContainerInstancesInput{
		ContainerInstances: []string{aws.ToString(foundTask.ContainerInstanceArn)},
		Cluster:            aws.String(cluster),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't get container instance")
		return 1, err
	}

	instance := contInstanceResult.ContainerInstances[0]
	instanceID := instance.Ec2InstanceId

	ec2Svc := ec2.NewFromConfig(sessionConfig)
	ec2Result, err := ec2Svc.DescribeInstances(context.TODO(), &ec2.DescribeInstancesInput{
		InstanceIds: []string{aws.ToString(instanceID)},
	})
	if err != nil {
		ctx.WithError(err).Error("Can't get ec2 instance")
		return 1, err
	}

	ec2Instance := ec2Result.Reservations[0].Instances[0]

	if pushSSHKey {
		ec2ICSvc := ec2instanceconnect.NewFromConfig(sessionConfig)

		ctx.WithField("instance_id", aws.ToString(ec2Instance.InstanceId)).Info("Pushing SSH key...")

		sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
		if err != nil {
			ctx.WithError(err).Error("Can't connect to the ssh agent")
			return 1, err
		}

		keys, err := agent.NewClient(sshAgent).List()
		if err != nil {
			ctx.WithError(err).Error("Can't get public keys from ssh agent. Please ensure you have the ssh-agent running")
			return 1, err
		}
		if len(keys) < 1 {
			ctx.Error("Can't get public keys from ssh agent. Please ensure you have at least one identity added (with ssh-add)")
			return 1, err
		}
		pubkey := keys[0].String()

		_, err = ec2ICSvc.SendSSHPublicKey(context.TODO(), &ec2instanceconnect.SendSSHPublicKeyInput{
			InstanceId:       ec2Instance.InstanceId,
			InstanceOSUser:   aws.String(instanceUser),
			AvailabilityZone: ec2Instance.Placement.AvailabilityZone,
			SSHPublicKey:     aws.String(pubkey),
		})
		if err != nil {
			ctx.WithError(err).Error("Can't push SSH key")
			return 1, err
		}
	}

	ctx.WithField("instance_id", aws.ToString(ec2Instance.InstanceId)).Info("Connecting to container...")

	params := []string{
		"ssh",
		"-tt",
		fmt.Sprintf("%s@%s.%s", instanceUser, aws.ToString(ec2Instance.PrivateIpAddress), profile),
		"docker-exec",
		aws.ToString(foundTask.TaskArn),
		containerName,
		shell,
	}

	env := os.Environ()

	return 0, syscall.Exec("/usr/bin/ssh", params, env)
}

