package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ssm"
)

func WriteSSMParameter(profile, parameterName, kmsKey, value string, processor []string) error {
	err := makeSession(profile)
	if err != nil {
		return err
	}
	// run the processor command, feed the value to its input and get the response
	if len(processor) > 0 {
		value, err = processInput(value, processor)
		if err != nil {
			return fmt.Errorf("processor command exited with an error: %s", err)
		}
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

func processInput(value string, command []string) (string, error) {
	log.Debugf("running command '%s'", strings.Join(command, " "))
	output := new(bytes.Buffer)

	cmd := exec.Command(command[0], command[1:]...)

	// get the stdin to write to
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	go func() {
		defer stdin.Close()
		io.WriteString(stdin, value)
	}()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	go func() {
		defer stdout.Close()
		io.Copy(output, stdout)
	}()

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	go func() {
		defer stderr.Close()
		io.Copy(os.Stderr, stderr)
	}()

	if err := cmd.Start(); err != nil {
		return "", err
	}
	if err := cmd.Wait(); err != nil {
		return "", err
	}

	// check if we still have a valid JSON file
	{
		out := make(map[string]interface{})
		err := json.Unmarshal(output.Bytes(), &out)
		if err != nil {
			return "", fmt.Errorf("got invalid json from the processor output: %s", err)
		}
	}

	return output.String(), nil
}
