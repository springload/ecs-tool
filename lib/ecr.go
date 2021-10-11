package lib

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// EcrLogin prints login cmd for docker
func EcrLogin(profile string) (err error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithSharedConfigProfile(profile))
	if err != nil {
		return err
	}
	svc := ecr.NewFromConfig(cfg)
	input := &ecr.GetAuthorizationTokenInput{}

	result, err := svc.GetAuthorizationToken(context.TODO(), input)
	if err != nil {
		return err
	}
	if n := len(result.AuthorizationData); n != 1 {
		return fmt.Errorf("Got %d authorizations instead of one", n)
	}
	auth := result.AuthorizationData[0]
	decodedToken, err := base64.StdEncoding.DecodeString(aws.ToString(auth.AuthorizationToken))
	if err != nil {
		return err
	}
	userPass := strings.SplitN(string(decodedToken), ":", 2)
	if n := len(userPass); n != 2 {
		return fmt.Errorf("Got %d user and password pards instead of two", n)
	}

	fmt.Println(strings.Join([]string{
		"docker",
		"login",
		"-u",
		userPass[0],
		"-p",
		userPass[1],
		aws.ToString(auth.ProxyEndpoint),
	}, " "))

	return nil
}

// EcrEndpoint prints endpoint for docker
func EcrEndpoint(profile string) (err error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithSharedConfigProfile(profile))
	if err != nil {
		return err
	}
	svc := sts.NewFromConfig(cfg)
	input := &sts.GetCallerIdentityInput{}
	result, err := svc.GetCallerIdentity(context.TODO(), input)
	if err != nil {
		return err
	}

	fmt.Println(strings.Join([]string{
		aws.ToString(result.Account),
		"dkr.ecr",
		aws.ToString(localSession.Config.Region),
		"amazonaws.com",
	}, "."))

	return nil
}
