package lib

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

func awsvpcService(status string, subnets, sgs []string, publicIP types.AssignPublicIp) types.Service {
	return types.Service{
		ServiceName: aws.String("app"),
		Status:      aws.String(status),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        subnets,
				SecurityGroups: sgs,
				AssignPublicIp: publicIP,
			},
		},
	}
}

func TestServiceNetworkConfigurationCopiesActiveService(t *testing.T) {
	out := &ecs.DescribeServicesOutput{Services: []types.Service{
		awsvpcService("ACTIVE", []string{"subnet-a", "subnet-b"}, []string{"sg-1"}, types.AssignPublicIpDisabled),
	}}

	got := serviceNetworkConfiguration(out)
	if got == nil {
		t.Fatal("expected the service's network configuration, got nil")
	}
	cfg := got.AwsvpcConfiguration
	if len(cfg.Subnets) != 2 || cfg.Subnets[0] != "subnet-a" || cfg.Subnets[1] != "subnet-b" {
		t.Fatalf("subnets not copied from the service, got %v", cfg.Subnets)
	}
	if len(cfg.SecurityGroups) != 1 || cfg.SecurityGroups[0] != "sg-1" {
		t.Fatalf("security groups not copied from the service, got %v", cfg.SecurityGroups)
	}
	if cfg.AssignPublicIp != types.AssignPublicIpDisabled {
		t.Fatalf("public ip setting not copied from the service, got %v", cfg.AssignPublicIp)
	}
}

func TestServiceNetworkConfigurationFallsBack(t *testing.T) {
	cases := map[string]*ecs.DescribeServicesOutput{
		"nil output":       nil,
		"service missing":  {Services: []types.Service{}},
		"service inactive": {Services: []types.Service{awsvpcService("INACTIVE", []string{"subnet-a"}, []string{"sg-1"}, types.AssignPublicIpEnabled)}},
		"no subnets":       {Services: []types.Service{awsvpcService("ACTIVE", nil, []string{"sg-1"}, types.AssignPublicIpEnabled)}},
		"not awsvpc":       {Services: []types.Service{{ServiceName: aws.String("app"), Status: aws.String("ACTIVE")}}},
	}
	for name, out := range cases {
		if got := serviceNetworkConfiguration(out); got != nil {
			t.Errorf("%s: expected nil so the tag lookup is used, got %+v", name, got)
		}
	}
}

func TestTaskStartFailure(t *testing.T) {
	failed := types.Task{
		StopCode:      types.TaskStopCodeTaskFailedToStart,
		StoppedReason: aws.String("ResourceInitializationError: unable to pull secrets or registry auth"),
	}
	if reason, ok := taskStartFailure(failed); !ok || reason != "ResourceInitializationError: unable to pull secrets or registry auth" {
		t.Fatalf("expected the stopped reason for a task that failed to start, got %q %v", reason, ok)
	}

	ran := types.Task{StopCode: types.TaskStopCodeEssentialContainerExited, StoppedReason: aws.String("Essential container in task exited")}
	if _, ok := taskStartFailure(ran); ok {
		t.Fatal("a task whose container ran and exited must not be reported as failing to start")
	}
}
