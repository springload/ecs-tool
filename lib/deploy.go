package lib

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// ecsDeployClient is the part of the ECS client a deploy uses. Tests pass a fake.
type ecsDeployClient interface {
	ecs.DescribeServicesAPIClient
	DescribeTaskDefinition(context.Context, *ecs.DescribeTaskDefinitionInput, ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error)
	RegisterTaskDefinition(context.Context, *ecs.RegisterTaskDefinitionInput, ...func(*ecs.Options)) (*ecs.RegisterTaskDefinitionOutput, error)
	UpdateService(context.Context, *ecs.UpdateServiceInput, ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error)
	DeregisterTaskDefinition(context.Context, *ecs.DeregisterTaskDefinitionInput, ...func(*ecs.Options)) (*ecs.DeregisterTaskDefinitionOutput, error)
}

// waitForStableService waits for the service to reach a steady state. Tests swap it out.
var waitForStableService = func(ctx context.Context, svc ecs.DescribeServicesAPIClient, input *ecs.DescribeServicesInput, timeout time.Duration) error {
	return ecs.NewServicesStableWaiter(svc).Wait(ctx, input, timeout)
}

// DeployServices deploys specified services in parallel
func DeployServices(profile, cluster, imageTag string, imageTags, services []string, workDir string, timeout time.Duration) (exitCode int, err error) {
	ctx := log.WithFields(log.Fields{
		"cluster":   cluster,
		"image_tag": imageTag,
	})

	err = InitAWS(profile)
	if err != nil {
		return 1, err
	}
	svc := sessionInstance
	exits := make(chan int, len(services))
	rollback := make(chan bool, len(services))

	var wg sync.WaitGroup
	for _, service := range services {
		service := service // go catch
		wg.Add(1)
		go func() {
			defer wg.Done()
			deployService(ctx, svc, timeout, cluster, imageTag, imageTags, workDir, service, exits, rollback, &wg)
		}()
	}

	for n := 0; n < len(services); n++ {
		if code := <-exits; code > 0 {
			exitCode = 127
			err = fmt.Errorf("one of the services failed to deploy")
		}
	}
	if exitCode != 0 {
		for n := 0; n < len(services); n++ {
			rollback <- true
		}
	} else {
		close(rollback)
	}

	wg.Wait()
	return
}

func deployService(ctx log.Interface, svc ecsDeployClient, timeout time.Duration, cluster, imageTag string, imageTags []string, workDir, service string, exitChan chan int, rollback chan bool, wg *sync.WaitGroup) {
	ctx = ctx.WithFields(log.Fields{
		"service": service,
	})
	ctx.Info("Deploying")

	// first, describe the service to get current task definition
	describeResult, err := svc.DescribeServices(context.TODO(), &ecs.DescribeServicesInput{
		Cluster:  aws.String(cluster),
		Services: []string{service},
	})
	if err != nil {
		ctx.WithError(err).Error("Can't describe service")
		exitChan <- 1
		return
	}
	if len(describeResult.Failures) > 0 {
		for _, failure := range describeResult.Failures {
			ctx.Errorf("%+v", failure)
		}
		exitChan <- 2
		return
	}

	// then describe the task definition to get a copy of it
	describeTaskResult, err := svc.DescribeTaskDefinition(context.TODO(), &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: describeResult.Services[0].TaskDefinition,
		Include:        []types.TaskDefinitionField{types.TaskDefinitionFieldTags},
	})
	if err != nil {
		ctx.WithError(err).Error("Can't get task definition")
		exitChan <- 3
		return
	}

	taskDefinition := describeTaskResult.TaskDefinition
	// replace the image tag if there is any
	if err := modifyContainerDefinitionImages(imageTag, imageTags, workDir, taskDefinition.ContainerDefinitions, ctx); err != nil {
		ctx.WithError(err).Error("Can't modify container definition images")
		exitChan <- 1
	}

	// now, register the new task
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
		Tags:                    nilIfEmpty(describeTaskResult.Tags),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't register task definition")
		exitChan <- 4
		return
	}
	ctx.WithField(
		"task_definition_arn",
		aws.ToString(registerResult.TaskDefinition.TaskDefinitionArn),
	).Debug("Registered the task definition")

	// now we are running DescribeService periodically to get the events
	doneChan := make(chan bool)
	defer func() { doneChan <- true }()

	wg.Add(1)
	go func(ctx log.Interface, cluster, service string) {
		last := time.Now()

		defer wg.Done()

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		printEvent := func(last time.Time) time.Time {
			describeResult, err := svc.DescribeServices(context.TODO(), &ecs.DescribeServicesInput{
				Cluster:  aws.String(cluster),
				Services: []string{service},
			})
			if err != nil {
				ctx.WithError(err).Error("Can't describe service")
				return last
			}
			for _, event := range describeResult.Services[0].Events {
				if !aws.ToTime(event.CreatedAt).Before(last) {
					ctx.Info(aws.ToString(event.Message))
					last = aws.ToTime(event.CreatedAt)
				}
			}

			return last
		}
		for {
			select {
			case <-doneChan:
				printEvent(last)
				return
			case <-ticker.C:
				last = printEvent(last)
			}
		}
	}(ctx, cluster, service)

	// update the service using the new registered task definition
	err = updateService(
		ctx,
		svc,
		timeout,
		aws.ToString(describeResult.Services[0].ClusterArn),
		aws.ToString(describeResult.Services[0].ServiceArn),
		aws.ToString(registerResult.TaskDefinition.TaskDefinitionArn),
	)

	wg.Add(1)
	// run the rollback function in background
	go func(ctx log.Interface) {
		defer wg.Done()
		if n, ok := <-rollback; n && ok {
			ctx.WithField(
				"task_definition_arn",
				aws.ToString(describeResult.Services[0].TaskDefinition),
			).Info("Rolling back to the previous task definition")
			if err := updateService(
				ctx,
				svc,
				timeout,
				aws.ToString(describeResult.Services[0].ClusterArn),
				aws.ToString(describeResult.Services[0].ServiceArn),
				aws.ToString(describeResult.Services[0].TaskDefinition),
			); err != nil {
				ctx.WithError(err).Error("Couldn't rollback.")
			}
		}
	}(ctx)

	var deregisterTaskArn *string
	if err != nil {
		ctx.WithError(err).Error("Couldn't deploy. Will try to roll back")
		deregisterTaskArn = registerResult.TaskDefinition.TaskDefinitionArn
		exitChan <- 5
	} else {
		deregisterTaskArn = describeTaskResult.TaskDefinition.TaskDefinitionArn
		exitChan <- 0
	}

	// deregister the old task definition
	ctx = ctx.WithFields(log.Fields{"task_definition_arn": aws.ToString(deregisterTaskArn)})
	ctx.Debug("Deregistered the task definition")
	_, err = svc.DeregisterTaskDefinition(context.TODO(), &ecs.DeregisterTaskDefinitionInput{
		TaskDefinition: deregisterTaskArn,
	})
	if err != nil {
		ctx.WithError(err).Error("Can't deregister task definition")
	}

}

func updateService(ctx log.Interface, svc ecsDeployClient, timeout time.Duration, cluster, service, taskDefinition string) error {
	// update the service using the new registered task definition
	_, err := svc.UpdateService(context.TODO(), &ecs.UpdateServiceInput{
		Cluster:        aws.String(cluster),
		Service:        aws.String(service),
		TaskDefinition: aws.String(taskDefinition),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't update the service")
		return err
	}
	ctx.Info("Updated the service")
	err = waitForStableService(context.TODO(), svc, &ecs.DescribeServicesInput{
		Cluster:  aws.String(cluster),
		Services: []string{service},
	}, timeout)
	if err != nil {
		ctx.WithError(err).Error("The waiter has been finished with an error")
		return err
	}

	ctx.Info("Service has been deployed")
	return nil
}
