package deploy

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecs"
)

func DeployServices(profile, cluster, imageTag string, services []string) (exitCode int, err error) {
	ctx := log.WithFields(log.Fields{
		"cluster":   cluster,
		"image_tag": imageTag,
	})

	err = makeSession(profile)
	if err != nil {
		return 1, err
	}
	exits := make(chan int, len(services))
	rollback := make(chan bool, len(services))

	var wg sync.WaitGroup
	for _, service := range services {
		service := service // go catch
		wg.Add(1)
		go func() {
			defer wg.Done()
			deployService(ctx, cluster, imageTag, service, exits, rollback, wg)
		}()
	}

	for n := 0; n < len(services); n++ {
		if code := <-exits; code > 0 {
			exitCode = 127
			err = fmt.Errorf("One of the services failed to deploy")
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

func deployService(ctx log.Interface, cluster, imageTag string, service string, exitChan chan int, rollback chan bool, wg sync.WaitGroup) {
	ctx = ctx.WithFields(log.Fields{
		"service": service,
	})
	ctx.Info("Deploying")

	svc := ecs.New(localSession)

	// first, describe the service to get current task definition
	describeResult, err := svc.DescribeServices(&ecs.DescribeServicesInput{
		Cluster:  aws.String(cluster),
		Services: aws.StringSlice([]string{service}),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't describe service")
		exitChan <- 1
		return
	}
	if len(describeResult.Failures) > 0 {
		for _, failure := range describeResult.Failures {
			ctx.Error(failure.GoString())
		}
		exitChan <- 2
		return
	}

	// then describe the task definition to get a copy of it
	describeTaskResult, err := svc.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{
		TaskDefinition: describeResult.Services[0].TaskDefinition,
	})
	if err != nil {
		ctx.WithError(err).Error("Can't get task definition")
		exitChan <- 3
		return
	}

	taskDefinition := describeTaskResult.TaskDefinition
	// replace the image tag if there is any
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
					"old_image_tag":  imageWithTag[1],
				}).Info("Image tag changed")
			}
		}
	}

	// now, register the new task
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
		exitChan <- 4
		return
	}
	ctx.WithField(
		"task_definition_arn",
		aws.StringValue(registerResult.TaskDefinition.TaskDefinitionArn),
	).Debug("Registered the task definition")

	// now we are running DescribeService periodically to get the events
	doneChan := make(chan bool)
	defer func() { doneChan <- true }()

	wg.Add(1)
	go func(ctx log.Interface, cluster, service string) {
		last := time.Now()

		defer wg.Done()
		svc := ecs.New(localSession)

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		printEvent := func(last time.Time) time.Time {
			describeResult, err := svc.DescribeServices(&ecs.DescribeServicesInput{
				Cluster:  aws.String(cluster),
				Services: aws.StringSlice([]string{service}),
			})
			if err != nil {
				ctx.WithError(err).Error("Can't describe service")
				return last
			}
			for _, event := range describeResult.Services[0].Events {
				if aws.TimeValue(event.CreatedAt).After(last) {
					ctx.Info(aws.StringValue(event.Message))
					last = aws.TimeValue(event.CreatedAt)
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
		aws.StringValue(describeResult.Services[0].ClusterArn),
		aws.StringValue(describeResult.Services[0].ServiceArn),
		aws.StringValue(registerResult.TaskDefinition.TaskDefinitionArn),
	)

	wg.Add(1)
	// run the rollback function in background
	go func(ctx log.Interface) {
		defer wg.Done()
		if n, ok := <-rollback; n && ok {
			ctx.WithField(
				"task_definition_arn",
				aws.StringValue(describeResult.Services[0].TaskDefinition),
			).Info("Rolling back to the previous task definition")
			if err := updateService(
				ctx,
				aws.StringValue(describeResult.Services[0].ClusterArn),
				aws.StringValue(describeResult.Services[0].ServiceArn),
				aws.StringValue(describeResult.Services[0].TaskDefinition),
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
	ctx = ctx.WithFields(log.Fields{"task_definition_arn": aws.StringValue(deregisterTaskArn)})
	ctx.Debug("Deregistered the task definition")
	_, err = svc.DeregisterTaskDefinition(&ecs.DeregisterTaskDefinitionInput{
		TaskDefinition: deregisterTaskArn,
	})
	if err != nil {
		ctx.WithError(err).Error("Can't deregister task definition")
	}

}

func updateService(ctx log.Interface, cluster, service, taskDefinition string) error {
	svc := ecs.New(localSession)
	// update the service using the new registered task definition
	_, err := svc.UpdateService(&ecs.UpdateServiceInput{
		Cluster:        aws.String(cluster),
		Service:        aws.String(service),
		TaskDefinition: aws.String(taskDefinition),
	})
	if err != nil {
		ctx.WithError(err).Error("Can't update the service")
		return err
	}
	ctx.Info("Updated the service")
	err = svc.WaitUntilServicesStable(&ecs.DescribeServicesInput{
		Cluster:  aws.String(cluster),
		Services: []*string{aws.String(service)},
	})
	if err != nil {
		ctx.WithError(err).Error("The waiter has been finished with an error")
		return err
	}

	ctx.Info("Service has been deployed")
	return nil
}
