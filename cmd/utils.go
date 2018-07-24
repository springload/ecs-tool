package cmd

import (
	"fmt"
	"os"
	"path"

	"github.com/apex/log"
)

func findConfigByEnvironment(environment string) (string, error) {
	var filename string
	dir, err := os.Getwd()

	if err != nil {
		return "", err
	}
	for dir != "/" {
		filename = path.Join(dir, "infra", fmt.Sprintf("ecs-%s.toml", environment))
		if _, err := os.Stat(filename); err == nil {
			log.WithFields(log.Fields{
				"config": filename,
			}).Debug("Found config!")
			return filename, nil
		}
		dir = path.Clean(path.Join(dir, ".."))
	}

	return "", fmt.Errorf("'infra/ecs-%s.toml' doesn't exist", environment)
}
