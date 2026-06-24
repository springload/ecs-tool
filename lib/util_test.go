package lib

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecs"
)

var testdata = map[string]string{
	"arn:aws:ecs:us-west-1:433844053624:task/scratchpower-staging/334083dd41c04b429cbf99ea7aeeef19": "334083dd41c04b429cbf99ea7aeeef19",     // new longer
	"arn:aws:ecs:ap-southeast-2:208168611618:task/0c5027c6-7bbd-476b-a534-adaf56760ca9":             "0c5027c6-7bbd-476b-a534-adaf56760ca9", // old format
}

func TestNilIfEmpty(t *testing.T) {
	if nilIfEmpty(nil) != nil {
		t.Fatal("nil input should return nil")
	}
	if nilIfEmpty([]*ecs.Tag{}) != nil {
		t.Fatal("empty slice should return nil")
	}
	tags := []*ecs.Tag{{Key: aws.String("env"), Value: aws.String("prod")}}
	result := nilIfEmpty(tags)
	if len(result) != 1 || aws.StringValue(result[0].Key) != "env" || aws.StringValue(result[0].Value) != "prod" {
		t.Fatalf("non-empty slice should be returned unchanged, got %v", result)
	}
}

func TestParseTaskUUID(t *testing.T) {
	for testArn, uuid := range testdata {
		if parsedUUID, err := parseTaskUUID(aws.String(testArn)); err != nil {
			t.Fatalf("Can't parse this ARN '%s': %s", testArn, err)
		} else {
			if parsedUUID == uuid {
				t.Logf("%s == %s", parsedUUID, uuid)
			} else {
				t.Fatalf("%s != %s", parsedUUID, uuid)
			}
		}
		t.Log(testArn, uuid)
	}
}
