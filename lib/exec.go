package lib

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/apex/log"
	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	ecsv2 "github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	ecsv1 "github.com/aws/aws-sdk-go/service/ecs"
	"github.com/fujiwara/ecsta"
)

// Error message constants for error detection
const (
	ErrUnknownFlag = "unknown shorthand flag"
	ErrNoFile      = "no such file or directory"
	ErrNotFound    = "not found"
	ErrForkExec    = "fork/exec"
)

var sessionInstance *ecsv2.Client
var sessionConfig awsv2.Config // Variable for session configuration

// InitAWS initializes a new AWS session with the specified profile for Ecsta realization
func InitAWS(profile string) error {
	if sessionInstance == nil {
		cfg, err := config.LoadDefaultConfig(context.TODO(),
			config.WithSharedConfigProfile(profile),
		)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
		os.Setenv("AWS_PROFILE", profile) //required for aws sdk
		sessionInstance = ecsv2.NewFromConfig(cfg)
		sessionConfig = cfg // Save session configuration
	}
	return nil
}

// getTaskDefinitionFromTaskID gets the task definition ARN from a task ID and extracts the family name
func getTaskDefinitionFromTaskID(profile, cluster, taskID string) (taskDefinitionName string, err error) {
	err = makeSession(profile)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	svc := ecsv1.New(localSession)

	// List tasks to find the one matching the task ID
	listResult, err := svc.ListTasks(&ecsv1.ListTasksInput{
		Cluster: aws.String(cluster),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list tasks: %w", err)
	}

	if len(listResult.TaskArns) == 0 {
		return "", fmt.Errorf("no tasks found in cluster")
	}

	// Find task that matches the task ID (task ID is usually a prefix of the full ARN)
	var matchingTaskArn *string
	for _, taskArn := range listResult.TaskArns {
		taskArnStr := aws.StringValue(taskArn)
		// Task ID is usually the last part of the ARN after the last /
		parts := strings.Split(taskArnStr, "/")
		if len(parts) > 0 {
			taskArnID := parts[len(parts)-1]
			// Check if task ID matches (task ID is always a prefix of the ARN ID)
			if strings.HasPrefix(taskArnID, taskID) {
				matchingTaskArn = taskArn
				break
			}
		}
	}

	if matchingTaskArn == nil {
		return "", fmt.Errorf("task ID %s not found in cluster", taskID)
	}

	// Describe the task to get task definition ARN
	describeResult, err := svc.DescribeTasks(&ecsv1.DescribeTasksInput{
		Cluster: aws.String(cluster),
		Tasks:   []*string{matchingTaskArn},
	})
	if err != nil {
		return "", fmt.Errorf("failed to describe task: %w", err)
	}

	if len(describeResult.Tasks) == 0 {
		return "", fmt.Errorf("task not found")
	}

	taskDefinitionArn := aws.StringValue(describeResult.Tasks[0].TaskDefinitionArn)

	// Extract task definition family name from ARN using proper ARN parsing
	// ARN format: arn:aws:ecs:region:account:task-definition/family:revision
	parsed, err := arn.Parse(taskDefinitionArn)
	if err != nil {
		return "", fmt.Errorf("invalid task definition ARN: %w", err)
	}

	// parsed.Resource == "task-definition/family:revision"
	parts := strings.Split(parsed.Resource, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid task definition ARN format: %s", taskDefinitionArn)
	}

	// Get family:revision and extract just the family name
	familyRevision := parts[1]
	familyParts := strings.Split(familyRevision, ":")
	taskDefinitionName = familyParts[0]

	return taskDefinitionName, nil
}

// extractEntrypointFromTaskDefinition extracts ssm-parent entrypoint and config from task definition
func extractEntrypointFromTaskDefinition(profile, cluster, taskDefinitionName, containerName string) (entrypoint string, configPath string, err error) {
	// Use AWS SDK v1 for compatibility with existing code
	err = makeSession(profile)
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}

	svc := ecsv1.New(localSession)

	describeResult, err := svc.DescribeTaskDefinition(&ecsv1.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(taskDefinitionName),
	})
	if err != nil {
		return "", "", fmt.Errorf("failed to describe task definition: %w", err)
	}

	// Find the container definition
	for _, containerDef := range describeResult.TaskDefinition.ContainerDefinitions {
		if aws.StringValue(containerDef.Name) == containerName {
			// Check EntryPoint field
			if len(containerDef.EntryPoint) > 0 {
				// EntryPoint is typically: ["/sbin/ssm-parent", "run", "-e", "-p", "...", "--", "su-exec", "www"]
				// We want to extract the ssm-parent path (first element) and config if present
				entrypoint = aws.StringValue(containerDef.EntryPoint[0])

				// Look for -c flag in EntryPoint to find config path
				for i, arg := range containerDef.EntryPoint {
					if i > 0 && aws.StringValue(arg) == "-c" && i+1 < len(containerDef.EntryPoint) {
						configPath = aws.StringValue(containerDef.EntryPoint[i+1])
						break
					}
				}

				return entrypoint, configPath, nil
			}
		}
	}

	return "", "", fmt.Errorf("container %s not found or has no entrypoint", containerName)
}

// SSMParentConfig holds the configuration for ssm-parent execution
type SSMParentConfig struct {
	EntrypointPaths     []string
	ConfigPaths         []string
	SupportsCFlag       bool
	ExtractionSucceeded bool
}

// execResult holds the result of an execution attempt
type execResult struct {
	succeeded bool
	err       error
}

// determineSSMParentConfig extracts entrypoint and config from task definition
// and determines the appropriate ssm-parent configuration
func determineSSMParentConfig(profile, cluster, taskDefinitionName, containerName string) SSMParentConfig {
	config := SSMParentConfig{
		EntrypointPaths: []string{},
		ConfigPaths:     []string{},
	}

	if taskDefinitionName != "" && containerName != "" {
		extractedEntrypoint, extractedConfig, err := extractEntrypointFromTaskDefinition(profile, cluster, taskDefinitionName, containerName)
		if err == nil {
			config.ExtractionSucceeded = true
			// Only try the extracted entrypoint - don't try unknown fallback paths
			// Unknown paths can kill the ECS Exec session if they don't exist
			config.EntrypointPaths = []string{extractedEntrypoint}

			// Detect -c flag support: if extractedConfig is not empty, container supports -c
			config.SupportsCFlag = (extractedConfig != "")

			if config.SupportsCFlag {
				// Container supports -c flag, use extracted config and fallbacks
				config.ConfigPaths = []string{
					extractedConfig, // Use extracted config first
					"/app/.ssm-parent.yaml",
					"/.ssm-parent.yaml",
					"",
				}
			} else {
				// Container does NOT support -c flag, skip all -c attempts
				config.ConfigPaths = []string{} // Empty - will skip -c format entirely
				log.Debug("Container does not support -c flag (not found in ENTRYPOINT), skipping all -c attempts")
			}

			log.WithFields(log.Fields{
				"entrypoint":      extractedEntrypoint,
				"config":          extractedConfig,
				"supports_c_flag": config.SupportsCFlag,
			}).Debug("Extracted entrypoint and config from task definition")
		} else {
			log.WithError(err).Debug("Could not extract entrypoint from task definition, skipping ssm-parent (will use direct exec)")
		}
	}

	// When extraction fails, skip ssm-parent entirely to avoid session kills
	// Unknown entrypoint paths can cause "fork/exec ... no such file" which kills the session
	if !config.ExtractionSucceeded {
		// Entrypoint extraction failed - skip ssm-parent, go straight to direct exec
		config.EntrypointPaths = []string{} // Empty - will skip ssm-parent entirely
		config.ConfigPaths = []string{}     // Empty - will skip -c format entirely
		log.Debug("Entrypoint extraction failed — skipping ssm-parent to avoid session kills, will use direct exec")
	}

	return config
}

// isUnknownFlagError checks if the error indicates an unknown flag
func isUnknownFlagError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, ErrUnknownFlag)
}

// isEntrypointNotFoundError checks if the error indicates the entrypoint was not found
func isEntrypointNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, ErrNoFile) ||
		strings.Contains(errMsg, ErrNotFound) ||
		strings.Contains(errMsg, ErrForkExec)
}

// resolveTaskDefinitionName extracts task definition name from task ID if needed
func resolveTaskDefinitionName(cfg ExecConfig) string {
	taskDefinitionName := cfg.TaskDefinitionName
	if cfg.TaskID != "" && taskDefinitionName == "" {
		extractedTaskDef, err := getTaskDefinitionFromTaskID(cfg.Profile, cfg.Cluster, cfg.TaskID)
		if err == nil {
			taskDefinitionName = extractedTaskDef
			log.WithFields(log.Fields{
				"task_id":         cfg.TaskID,
				"task_definition": taskDefinitionName,
			}).Debug("Extracted task definition from task ID")
		} else {
			log.WithError(err).Debug("Could not extract task definition from task ID, will use path fallback")
		}
	}
	return taskDefinitionName
}

// createEcstaApp creates and initializes the ecsta application
func createEcstaApp(cfg ExecConfig) (*ecsta.Ecsta, error) {
	if err := InitAWS(cfg.Profile); err != nil {
		return nil, fmt.Errorf("failed to initialize AWS session: %w", err)
	}

	// Get region from session config (already a string in awsv2.Config)
	regionStr := sessionConfig.Region
	ecstaApp, err := ecsta.New(context.TODO(), regionStr, cfg.Cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to create ecsta application: %w", err)
	}
	return ecstaApp, nil
}

// trySSMParentWithConfig attempts to execute command using ssm-parent with -c flag
func trySSMParentWithConfig(ecstaApp *ecsta.Ecsta, entrypoint string, configPaths []string, command string) execResult {
	for _, configPath := range configPaths {
		if configPath == "" {
			continue // Skip empty config path when trying -c format
		}

		fullCommand := fmt.Sprintf("%s -c %s run -- %s", entrypoint, configPath, command)

		execOpt := ecsta.ExecOption{
			Command: fullCommand,
		}

		ctx := log.WithFields(log.Fields{
			"entrypoint":  entrypoint,
			"config_path": configPath,
			"format":      "with -c flag",
			"command":     fullCommand,
		})
		ctx.Debug("Attempting to execute command with ssm-parent (-c flag format)")

		if err := ecstaApp.RunExec(context.Background(), &execOpt); err != nil {
			// If we get "unknown shorthand flag", this ssm-parent doesn't support -c flag
			if isUnknownFlagError(err) {
				ctx.WithError(err).Debug("ssm-parent doesn't support -c flag, skipping remaining configs and will try without -c")
				return execResult{succeeded: false, err: err}
			}
			// If entrypoint not found, break config loop and move to next entrypoint
			if isEntrypointNotFoundError(err) {
				ctx.WithError(err).Debug("entrypoint not found, breaking config loop")
				return execResult{succeeded: false, err: err}
			}
			// Other errors: try without -c flag for this entrypoint
			ctx.WithError(err).Debug("ssm-parent -c format failed with other error, will try without -c")
			return execResult{succeeded: false, err: err}
		}

		// RunExec returned nil - command may have succeeded or failed silently
		log.Debug("ssm-parent -c format attempt returned success (may have succeeded or failed silently)")
		return execResult{succeeded: true, err: nil}
	}

	// No config paths worked
	return execResult{succeeded: false, err: fmt.Errorf("no valid config paths")}
}

// trySSMParentWithoutConfig attempts to execute command using ssm-parent without -c flag
func trySSMParentWithoutConfig(ecstaApp *ecsta.Ecsta, entrypoint string, command string) execResult {
	fullCommand := fmt.Sprintf("%s run -- %s", entrypoint, command)

	execOpt := ecsta.ExecOption{
		Command: fullCommand,
	}

	ctx := log.WithFields(log.Fields{
		"entrypoint": entrypoint,
		"format":     "without -c flag",
		"command":    fullCommand,
	})
	ctx.Debug("Attempting to execute command with ssm-parent (simple format)")

	if err := ecstaApp.RunExec(context.Background(), &execOpt); err != nil {
		// If entrypoint not found, try next entrypoint path
		if isEntrypointNotFoundError(err) {
			ctx.WithError(err).Debug("ssm-parent not found, trying next entrypoint path")
			return execResult{succeeded: false, err: err}
		}
		// Other errors: also try next entrypoint
		ctx.WithError(err).Debug("ssm-parent execution failed, trying next entrypoint")
		return execResult{succeeded: false, err: err}
	}

	// RunExec returned nil - command may have succeeded or failed silently
	log.Debug("ssm-parent simple format attempt returned success (may have succeeded or failed silently)")
	return execResult{succeeded: true, err: nil}
}

// trySSMParent attempts to execute command using ssm-parent with all available entrypoints
func trySSMParent(ecstaApp *ecsta.Ecsta, ssmConfig SSMParentConfig, command string) execResult {
	entrypointPaths := ssmConfig.EntrypointPaths
	configPaths := ssmConfig.ConfigPaths

	if len(entrypointPaths) == 0 {
		// No entrypoints to try
		return execResult{succeeded: false, err: nil}
	}

	var lastErr error

	for _, entrypoint := range entrypointPaths {
		var entrypointErr error

		// First, try with -c flag format if config paths are available
		if len(configPaths) > 0 {
			result := trySSMParentWithConfig(ecstaApp, entrypoint, configPaths, command)
			if result.succeeded {
				return result
			}
			// If unknown flag error, skip remaining configs and try without -c
			if result.err != nil && isUnknownFlagError(result.err) {
				// Try without -c flag for this entrypoint
				result = trySSMParentWithoutConfig(ecstaApp, entrypoint, command)
				if result.succeeded {
					return result
				}
				entrypointErr = result.err
				// If entrypoint not found, try next entrypoint
				if result.err != nil && isEntrypointNotFoundError(result.err) {
					lastErr = entrypointErr
					continue
				}
				// Other errors: also try next entrypoint
				lastErr = entrypointErr
				continue
			}
			// If entrypoint not found, try next entrypoint
			if result.err != nil && isEntrypointNotFoundError(result.err) {
				lastErr = result.err
				continue
			}
			// Store error for potential use if without -c also fails
			entrypointErr = result.err
		}

		// Try without -c flag format
		result := trySSMParentWithoutConfig(ecstaApp, entrypoint, command)
		if result.succeeded {
			return result
		}
		// Store error for final return if all attempts fail
		if result.err != nil {
			lastErr = result.err
			// If entrypoint not found, try next entrypoint
			if isEntrypointNotFoundError(result.err) {
				continue
			}
		} else if entrypointErr != nil {
			// Use error from -c attempt if available
			lastErr = entrypointErr
		}
	}

	// All attempts failed
	return execResult{succeeded: false, err: lastErr}
}

// tryDirectExecution attempts to execute command directly without ssm-parent
func tryDirectExecution(ecstaApp *ecsta.Ecsta, command string, ssmTried, ssmSucceeded bool) execResult {
	// Log appropriate debug message
	if ssmTried {
		if ssmSucceeded {
			log.Debug("ssm-parent appears to have succeeded, but attempting direct execution to ensure working session")
		} else {
			log.Debug("Attempting direct execution to verify/fallback from ssm-parent (handling silent failures)")
		}
	} else {
		log.Debug("Attempting to execute command directly (ssm-parent not available)")
	}

	execOpt := ecsta.ExecOption{
		Command: command,
	}

	if err := ecstaApp.RunExec(context.Background(), &execOpt); err != nil {
		return execResult{succeeded: false, err: err}
	}

	return execResult{succeeded: true, err: nil}
}

// handleExecutionResults determines the final outcome from ssm-parent and direct execution results
// Note: ecsta.RunExec may return nil even when the command inside the container fails silently.
// We handle this by always attempting direct execution as a fallback, which provides a working
// interactive session even if ssm-parent failed silently.
func handleExecutionResults(ssmResult, directResult execResult) error {
	// Always prefer direct execution result if it succeeded
	if directResult.succeeded {
		// Log appropriate message based on whether ssm-parent was attempted
		if ssmResult.succeeded {
			log.Info("Command executed successfully (direct execution - ssm-parent may have also worked)")
		} else if ssmResult.err != nil {
			log.Info("Command executed successfully (direct execution - ssm-parent may have failed silently)")
		} else {
			// ssmResult.err == nil and !succeeded means no ssm-parent attempt was made
			log.Info("Command executed successfully (direct execution)")
		}
		return nil
	}

	// Direct execution failed
	if ssmResult.err != nil {
		return fmt.Errorf("failed to execute command: ssm-parent error (%v), direct execution error (%w)", ssmResult.err, directResult.err)
	}
	return fmt.Errorf("failed to execute command: %w", directResult.err)
}

// ExecConfig holds configuration for ExecFargate
type ExecConfig struct {
	Profile            string
	Cluster            string
	Command            string
	TaskID             string
	TaskDefinitionName string
	ContainerName      string
}

// ExecFargate executes a command in a specified container on an ECS Fargate service
// taskID, taskDefinitionName and containerName are optional - if provided, will extract entrypoint from task definition
func ExecFargate(cfg ExecConfig) error {
	// Setup
	ecstaApp, err := createEcstaApp(cfg)
	if err != nil {
		return err
	}

	taskDefName := resolveTaskDefinitionName(cfg)
	ssmConfig := determineSSMParentConfig(cfg.Profile, cfg.Cluster, taskDefName, cfg.ContainerName)

	// Execute
	ssmResult := trySSMParent(ecstaApp, ssmConfig, cfg.Command)
	ssmTried := len(ssmConfig.EntrypointPaths) > 0
	ssmSucceeded := ssmResult.succeeded
	directResult := tryDirectExecution(ecstaApp, cfg.Command, ssmTried, ssmSucceeded)

	// Handle results
	return handleExecutionResults(ssmResult, directResult)
}
