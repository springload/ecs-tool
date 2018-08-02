package deploy

import (
	"fmt"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
)

var localSession *session.Session

func makeSession(profile string) error {
	if localSession == nil {
		log.Debug("Creating session")
		var err error
		// create AWS session
		localSession, err = session.NewSessionWithOptions(session.Options{
			Config: aws.Config{},

			SharedConfigState: session.SharedConfigEnable,
			Profile:           profile,
		})
		if err != nil {
			return fmt.Errorf("Can't get aws session.")
		}
	}
	return nil
}
