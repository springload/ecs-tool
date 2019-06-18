package cmd

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"

	"github.com/apex/log"
)

const (
	globPattern  = "ecs-*.toml"
	configFormat = "ecs-%s.toml"
)

var (
	rePattern = regexp.MustCompile(`^ecs-(\w+)\.toml$`)
)

func findInfraDir() (string, error) {
	var infra string
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir != path.Dir(dir) {
		infra = path.Join(dir, "infra")
		if stat, err := os.Stat(infra); err == nil {
			if stat.IsDir() { // directory infra found
				if files, _ := filepath.Glob(path.Join(infra, globPattern)); len(files) > 0 {
					return infra, nil
				}
			}
		}
		dir = path.Dir(dir)
	}

	return "", fmt.Errorf("Can't find directory with config files")

}

func findEnvironments() ([]string, error) {
	var envs []string
	infra, err := findInfraDir()
	if err != nil {
		return []string{}, err
	}
	if files, _ := filepath.Glob(path.Join(infra, globPattern)); len(files) > 0 {
		for _, fp := range files {
			file := filepath.Base(fp)
			match := rePattern.FindStringSubmatch(file)
			if match != nil {
				envs = append(envs, match[1])
			}
		}
		return envs, nil
	}

	return []string{}, fmt.Errorf("can't find any environment")
}

func findConfigByEnvironment(environment string) (string, error) {
	var filename string
	infra, err := findInfraDir()
	if err != nil {
		return "", err
	}
	filename = path.Join(infra, fmt.Sprintf(configFormat, environment))
	if _, err := os.Stat(filename); err == nil {
		log.WithFields(log.Fields{
			"config": filename,
		}).Debug("Found config!")
		return filename, nil
	}

	return "", fmt.Errorf("'infra/ecs-%s.toml' doesn't exist", environment)
}
