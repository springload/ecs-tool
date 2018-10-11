package deploy

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/aws/aws-sdk-go/service/sts"
)

// gets login cmd for docker
func EcrLogin(profile string) (err error) {
	err = makeSession(profile)
	if err != nil {
		return err
	}
	svc := ecr.New(localSession)
	input := &ecr.GetAuthorizationTokenInput{}

	result, err := svc.GetAuthorizationToken(input)
	if err != nil {
		return err
	}
	if n := len(result.AuthorizationData); n != 1 {
		return fmt.Errorf("Got %d authorizations instead of one", n)
	}
	auth := result.AuthorizationData[0]
	decodedToken, err := base64.StdEncoding.DecodeString(aws.StringValue(auth.AuthorizationToken))
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
		aws.StringValue(auth.ProxyEndpoint),
	}, " "))

	return nil
}

// gets endpoint for docker
func EcrEndpoint(profile string) (err error) {
	err = makeSession(profile)
	if err != nil {
		return err
	}
	svc := sts.New(localSession)
	input := &sts.GetCallerIdentityInput{}
	result, err := svc.GetCallerIdentity(input)
	if err != nil {
		return err
	}

	fmt.Println(strings.Join([]string{
		aws.StringValue(result.Account),
		"dkr.ecr",
		aws.StringValue(localSession.Config.Region),
		"amazonaws.com",
	}, "."))

	return nil
}
