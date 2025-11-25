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
	ecsv1 "github.com/aws/aws-sdk-go/service/ecs"
	"github.com/fujiwara/ecsta"
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
            // Check if task ID matches (could be full ID or partial)
            if strings.HasPrefix(taskArnID, taskID) || strings.HasPrefix(taskID, taskArnID) {
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
    
    // Extract task definition family name from ARN
    // ARN format: arn:aws:ecs:region:account:task-definition/family:revision
    parts := strings.Split(taskDefinitionArn, "/")
    if len(parts) < 2 {
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

// ExecFargate executes a command in a specified container on an ECS Fargate service
// taskID, taskDefinitionName and containerName are optional - if provided, will extract entrypoint from task definition
func ExecFargate(profile, cluster, command string, taskID, taskDefinitionName, containerName string) error {
    
    if err := InitAWS(profile); err != nil {
        return fmt.Errorf("failed to initialize AWS session: %w", err)
    }

    // Get region from session config (already a string in awsv2.Config)
    regionStr := sessionConfig.Region
    ecstaApp, err := ecsta.New(context.TODO(), regionStr, cluster)
    if err != nil {
        return fmt.Errorf("failed to create ecsta application: %w", err)
    }

    // Try to get task definition from task ID if provided
    if taskID != "" && taskDefinitionName == "" {
        extractedTaskDef, err := getTaskDefinitionFromTaskID(profile, cluster, taskID)
        if err == nil {
            taskDefinitionName = extractedTaskDef
            log.WithFields(log.Fields{
                "task_id": taskID,
                "task_definition": taskDefinitionName,
            }).Debug("Extracted task definition from task ID")
        } else {
            log.WithError(err).Debug("Could not extract task definition from task ID, will use path fallback")
        }
    }
    
    // Try to extract entrypoint and config from task definition if provided
    var entrypointPaths []string
    var configPaths []string
    extractionSucceeded := false
    
    if taskDefinitionName != "" && containerName != "" {
        extractedEntrypoint, extractedConfig, err := extractEntrypointFromTaskDefinition(profile, cluster, taskDefinitionName, containerName)
        if err == nil {
            extractionSucceeded = true
            // Only try the extracted entrypoint - don't try unknown fallback paths
            // Unknown paths can kill the ECS Exec session if they don't exist
            entrypointPaths = []string{extractedEntrypoint}
            
            // Detect -c flag support: if extractedConfig is not empty, container supports -c
            supportsCFlag := (extractedConfig != "")
            
            if supportsCFlag {
                // Container supports -c flag, use extracted config and fallbacks
                configPaths = []string{
                    extractedConfig,  // Use extracted config first
                    "/app/.ssm-parent.yaml",
                    "/.ssm-parent.yaml",
                    "",
                }
            } else {
                // Container does NOT support -c flag, skip all -c attempts
                configPaths = []string{} // Empty - will skip -c format entirely
                log.Debug("Container does not support -c flag (not found in ENTRYPOINT), skipping all -c attempts")
            }
            
            log.WithFields(log.Fields{
                "entrypoint": extractedEntrypoint,
                "config": extractedConfig,
                "supports_c_flag": supportsCFlag,
            }).Debug("Extracted entrypoint and config from task definition")
        } else {
            log.WithError(err).Debug("Could not extract entrypoint from task definition, skipping ssm-parent (will use direct exec)")
        }
    }
    
    // When extraction fails, skip ssm-parent entirely to avoid session kills
    // Unknown entrypoint paths can cause "fork/exec ... no such file" which kills the session
    if !extractionSucceeded {
        // Entrypoint extraction failed - skip ssm-parent, go straight to direct exec
        entrypointPaths = []string{} // Empty - will skip ssm-parent entirely
        configPaths = []string{} // Empty - will skip -c format entirely
        log.Debug("Entrypoint extraction failed — skipping ssm-parent to avoid session kills, will use direct exec")
    }

    var ssmParentTried bool
    var ssmParentErr error
    var ssmParentSucceeded bool
    
    // Try with ssm-parent first (preferred for env vars)
    // Support both formats: with -c flag (springload) and without (madewithwagtail)
    // Each entrypoint/config combination is tried exactly once
    for _, entrypoint := range entrypointPaths {
        ssmParentTried = true
        var cFlagNotSupported bool
        var entrypointFound bool
        var triedWithoutCFlag bool
        
        // First, try with -c flag format (for projects like springload)
        // Format: ssm-parent -c .ssm-parent.yaml run -- <command>
        // Only try if we have config paths and haven't determined -c is unsupported
        if !cFlagNotSupported && len(configPaths) > 0 {
            for _, configPath := range configPaths {
                if configPath == "" {
                    continue // Skip empty config path when trying -c format
                }
                
                // If we already determined -c is not supported, skip remaining config paths
                if cFlagNotSupported {
                    break
                }
                
                fullCommand := fmt.Sprintf("%s -c %s run -- %s", entrypoint, configPath, command)
                
                execOpt := ecsta.ExecOption{
                    Command: fullCommand,
                }
                
                ctx := log.WithFields(log.Fields{
                    "entrypoint": entrypoint,
                    "config_path": configPath,
                    "format": "with -c flag",
                    "command": fullCommand,
                })
                ctx.Debug("Attempting to execute command with ssm-parent (-c flag format)")
                
                if err := ecstaApp.RunExec(context.Background(), &execOpt); err != nil {
                    ssmParentErr = err
                    errMsg := strings.ToLower(err.Error())
                    // If we get "unknown shorthand flag", this ssm-parent doesn't support -c flag
                    // Skip all remaining config paths and try without -c for this entrypoint
                    if strings.Contains(errMsg, "unknown shorthand flag") {
                        ctx.WithError(err).Debug("ssm-parent doesn't support -c flag, skipping remaining configs and will try without -c")
                        cFlagNotSupported = true
                        break // Break out of config loop immediately
                    }
                    // If entrypoint not found, break config loop and move to next entrypoint
                    // (trying other config paths is pointless since the binary itself is missing)
                    if strings.Contains(errMsg, "no such file or directory") || 
                       strings.Contains(errMsg, "not found") ||
                       strings.Contains(errMsg, "fork/exec") {
                        ctx.WithError(err).Debug("entrypoint not found, breaking config loop (will try without -c, then next entrypoint if that also fails)")
                        break // Break config loop - we'll try without -c for this entrypoint, then move to next if that fails
                    }
                    // Other errors: try without -c flag for this entrypoint
                    ctx.WithError(err).Debug("ssm-parent -c format failed with other error, will try without -c")
                    cFlagNotSupported = true
                    break
                } else {
                    // RunExec returned nil - command may have succeeded or failed silently
                    // Mark that we tried this entrypoint and it exists
                    entrypointFound = true
                    ssmParentSucceeded = true // Track that ssm-parent didn't error (may have worked)
                    log.Debug("ssm-parent -c format attempt returned success (may have succeeded or failed silently)")
                    // Break out of config loop - we've found a working entrypoint/config combination
                    // We'll still try direct execution as fallback to ensure working session
                    break
                }
            }
        }
        
        // Second, try without -c flag format (for projects like madewithwagtail)
        // Format: ssm-parent run -- <command>
        // Try this if:
        // 1. -c flag is not supported (detected above)
        // 2. No config paths available
        // 3. We haven't tried without -c yet for this entrypoint
        if !triedWithoutCFlag && (cFlagNotSupported || len(configPaths) == 0 || (len(configPaths) == 1 && configPaths[0] == "")) {
            triedWithoutCFlag = true
            fullCommand := fmt.Sprintf("%s run -- %s", entrypoint, command)
            
            execOpt := ecsta.ExecOption{
                Command: fullCommand,
            }
            
            ctx := log.WithFields(log.Fields{
                "entrypoint": entrypoint,
                "format": "without -c flag",
                "command": fullCommand,
            })
            ctx.Debug("Attempting to execute command with ssm-parent (simple format)")
            
            if err := ecstaApp.RunExec(context.Background(), &execOpt); err != nil {
                ssmParentErr = err
                errMsg := strings.ToLower(err.Error())
                // If entrypoint not found, try next entrypoint path
                if strings.Contains(errMsg, "no such file or directory") || 
                   strings.Contains(errMsg, "not found") ||
                   strings.Contains(errMsg, "fork/exec") {
                    ctx.WithError(err).Debug("ssm-parent not found, trying next entrypoint path")
                    continue // Move to next entrypoint
                }
                // Other errors: also try next entrypoint
                ctx.WithError(err).Debug("ssm-parent execution failed, trying next entrypoint")
                continue
            } else {
                // RunExec returned nil - command may have succeeded or failed silently
                entrypointFound = true
                ssmParentSucceeded = true // Track that ssm-parent didn't error (may have worked)
                log.Debug("ssm-parent simple format attempt returned success (may have succeeded or failed silently)")
                // Continue to direct execution fallback to ensure working session
            }
        }
        
        // If we found the entrypoint (even if execution may have failed silently),
        // we've tried both formats for this entrypoint, so move on
        if entrypointFound {
            // We've exhausted options for this entrypoint, continue to direct execution
            break
        }
    }
    
    // Always try direct execution as fallback to handle silent failures
    // This ensures we get a working session even if ssm-parent failed silently
    // According to plan: "Always try direct execution after ssm-parent attempts (even if they return success)"
    if ssmParentTried {
        if ssmParentSucceeded {
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
        if ssmParentErr != nil {
            return fmt.Errorf("failed to execute command: ssm-parent error (%v), direct execution error (%w)", ssmParentErr, err)
        }
        return fmt.Errorf("failed to execute command: %w", err)
    }
    
    // Direct execution succeeded - this ensures we have a working session
    // Note: If ssm-parent also worked, we prefer direct execution here because
    // we can't verify ssm-parent actually worked due to silent failures
    if ssmParentTried {
        if ssmParentSucceeded {
            log.Info("Command executed successfully (direct execution - ssm-parent may have also worked)")
        } else {
            log.Info("Command executed successfully (direct execution - ssm-parent may have failed silently)")
        }
    } else {
        log.Info("Command executed successfully (direct execution)")
    }
    return nil
}
