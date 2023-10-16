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
	"github.com/imdario/mergo"
)

func WriteSSMParameter(profile, parameterName, kmsKey, value string, processor []string, pickJsonKeys []string) error {
	err := makeSession(profile)
	if err != nil {
		return err
	}
	// run the processor command, feed the value to its input and get the response
	if len(processor) > 0 {
		value, err = runProcessor(value, processor)
		if err != nil {
			return fmt.Errorf("processor command exited with an error: %s", err)
		}
	}

	if len(pickJsonKeys) > 0 {
		value, err = pickJsonKeysFromSerialised(value, pickJsonKeys)
		if err != nil {
			return fmt.Errorf("json picker exited with an error: %s", err)
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

func runProcessor(value string, command []string) (string, error) {
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
		if _, err := io.WriteString(stdin, value); err != nil {
			log.WithError(err).Error("can't write to stdin of the command")
		}
	}()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	go func() {
		defer stdout.Close()
		if _, err := io.Copy(output, stdout); err != nil {
			log.WithError(err).Error("can't write the command's stdout to output")
		}
	}()

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	go func() {
		defer stderr.Close()
		if _, err := io.Copy(os.Stderr, stderr); err != nil {
			log.WithError(err).Error("can't write the command's stderr to stderr")
		}
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

func pickJsonKeysFromSerialised(input string, keys []string) (output string, err error) {
	// first parse the top level json
	var parsedJson map[string]json.RawMessage
	var resultingStructure map[string]string

	err = json.Unmarshal([]byte(input), &parsedJson)
	if err != nil {
		return input, err
	}
	// pick the keys
	for _, key := range keys {
		if value, ok := parsedJson[key]; ok {
			// continue parsing the key
			// the picked keys should be maps of strings with string values
			var parsed map[string]string
			if err := json.Unmarshal(value, &parsed); err != nil {
				return input, fmt.Errorf("couldn't parse key %s in the ejson file: %s", key, err)
			}
			// merge the parsed map into the resulting structure
			if err := mergo.Map(&resultingStructure, parsed, mergo.WithOverride); err != nil {
				return input, fmt.Errorf("couldn't merge key %s into the resulting structure: %s", key, err)
			}
		} else {
			return input, fmt.Errorf("couldn't find %s key in the ejson file", key)
		}
	}
	if outputBytes, err := json.Marshal(resultingStructure); err != nil {
		return input, fmt.Errorf("couldn't marshal resulting structure '%v'", resultingStructure)
	} else {
		output = string(outputBytes)
	}

	return output, nil
}
