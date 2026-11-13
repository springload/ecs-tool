package lib

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

type fakeDeployClient struct{}

func (fakeDeployClient) DescribeServices(context.Context, *ecs.DescribeServicesInput, ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
	return &ecs.DescribeServicesOutput{Services: []types.Service{{
		ClusterArn:     aws.String("arn:cluster"),
		ServiceArn:     aws.String("arn:service"),
		TaskDefinition: aws.String("arn:old-td"),
	}}}, nil
}

func (fakeDeployClient) DescribeTaskDefinition(context.Context, *ecs.DescribeTaskDefinitionInput, ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error) {
	return &ecs.DescribeTaskDefinitionOutput{TaskDefinition: &types.TaskDefinition{
		TaskDefinitionArn: aws.String("arn:old-td"),
		Family:            aws.String("app"),
	}}, nil
}

func (fakeDeployClient) RegisterTaskDefinition(context.Context, *ecs.RegisterTaskDefinitionInput, ...func(*ecs.Options)) (*ecs.RegisterTaskDefinitionOutput, error) {
	return &ecs.RegisterTaskDefinitionOutput{TaskDefinition: &types.TaskDefinition{
		TaskDefinitionArn: aws.String("arn:new-td"),
	}}, nil
}

func (fakeDeployClient) UpdateService(context.Context, *ecs.UpdateServiceInput, ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error) {
	return &ecs.UpdateServiceOutput{}, nil
}

func (fakeDeployClient) DeregisterTaskDefinition(context.Context, *ecs.DeregisterTaskDefinitionInput, ...func(*ecs.Options)) (*ecs.DeregisterTaskDefinitionOutput, error) {
	return &ecs.DeregisterTaskDefinitionOutput{}, nil
}

// recordWaits swaps the stable-service waiter for one that records the timeout it receives.
// The returned func yields the recorded timeouts and is safe to call after the waits finish.
func recordWaits(t *testing.T, waitErr error) func() []time.Duration {
	t.Helper()
	var mu sync.Mutex
	var timeouts []time.Duration
	original := waitForStableService
	waitForStableService = func(_ context.Context, _ ecs.DescribeServicesAPIClient, _ *ecs.DescribeServicesInput, timeout time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		timeouts = append(timeouts, timeout)
		return waitErr
	}
	t.Cleanup(func() { waitForStableService = original })
	return func() []time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return append([]time.Duration(nil), timeouts...)
	}
}

func TestUpdateServicePassesTimeoutToWaiter(t *testing.T) {
	timeouts := recordWaits(t, nil)

	if err := updateService(log.Log, fakeDeployClient{}, 20*time.Minute, "cluster", "service", "td"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := timeouts()
	if len(got) != 1 || got[0] != 20*time.Minute {
		t.Fatalf("expected the waiter to get 20m, got %v", got)
	}
}

func TestDeployServiceUsesTimeoutForDeployAndRollbackWaits(t *testing.T) {
	timeouts := recordWaits(t, errors.New("service never stabilised"))

	exits := make(chan int, 1)
	rollback := make(chan bool, 1)
	rollback <- true
	var wg sync.WaitGroup

	deployService(log.Log, fakeDeployClient{}, 25*time.Minute, "cluster", "", nil, "", "app", exits, rollback, &wg)
	wg.Wait()

	got := timeouts()
	if len(got) != 2 {
		t.Fatalf("expected a deploy wait and a rollback wait, got %d waits: %v", len(got), got)
	}
	for i, timeout := range got {
		if timeout != 25*time.Minute {
			t.Fatalf("wait %d got timeout %v, expected 25m", i, timeout)
		}
	}
}
