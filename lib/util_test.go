package lib

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

var testdata = map[string]string{
	"arn:aws:ecs:us-west-1:433844053624:task/scratchpower-staging/334083dd41c04b429cbf99ea7aeeef19": "334083dd41c04b429cbf99ea7aeeef19",     // new longer
	"arn:aws:ecs:ap-southeast-2:208168611618:task/0c5027c6-7bbd-476b-a534-adaf56760ca9":             "0c5027c6-7bbd-476b-a534-adaf56760ca9", // old format
}

func TestParseTaskUUID(t *testing.T) {
	for testArn, uuid := range testdata {
		if parsedUUID, err := parseTaskUUID(aws.ToString(testArn)); err != nil {
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
