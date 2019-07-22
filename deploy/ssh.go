package deploy

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

func SSH(profile, cluster, taskDefinitionName, shell, service, instance_user string) (exitCode int, err error) {
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
	instanceId := instance.Ec2InstanceId

	ec2Svc := ec2.New(localSession)
	ec2Result, err := ec2Svc.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{instanceId},
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
	if err != nil {
		ctx.WithError(err).Error("Can't get public keys from ssh agent")
		return 1, err
	}
	pubkey := keys[0].String()

	_, err = ec2ICSvc.SendSSHPublicKey(&ec2instanceconnect.SendSSHPublicKeyInput{
		InstanceId:       ec2Instance.InstanceId,
		InstanceOSUser:   aws.String(instance_user),
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
		fmt.Sprintf("%s@%s", instance_user, aws.StringValue(ec2Instance.PrivateIpAddress)),
		"docker-exec",
		aws.StringValue(foundTask.TaskArn),
		shell}

	env := os.Environ()

	syscall.Exec("/usr/bin/ssh", params, env)

	return 0, nil
}
