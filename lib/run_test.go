package lib

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ecs"
)

func TestWaitForTasksStoppedPassesTimeoutToWaiter(t *testing.T) {
	var got time.Duration
	original := stoppedTasksWaiter
	stoppedTasksWaiter = func(_ context.Context, _ ecs.DescribeTasksAPIClient, _ *ecs.DescribeTasksInput, timeout time.Duration) error {
		got = timeout
		return nil
	}
	t.Cleanup(func() { stoppedTasksWaiter = original })

	if err := waitForTasksStopped(nil, &ecs.DescribeTasksInput{}, 45*time.Minute); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 45*time.Minute {
		t.Fatalf("expected the waiter to get 45m, got %v", got)
	}
}
