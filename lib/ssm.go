package lib

import (
	"github.com/apex/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ssm"
)

func WriteSSMParameter(profile, parameterName, kmsKey, value string) error {
	err := makeSession(profile)
	if err != nil {
		return err
	}
	svc := ssm.New(localSession)
	resp, err := svc.PutParameter(&ssm.PutParameterInput{
		Name:      aws.String(parameterName),
		Type:      aws.String(ssm.ParameterTypeSecureString),
		KeyId:     aws.String(kmsKey),
		Value:     aws.String(value),
		Overwrite: aws.Bool(true),
	})
	if err != nil {
		return err
	}
	log.Infof("wrote parameter '%s' version %d", parameterName, aws.Int64Value(resp.Version))

	return nil
}
