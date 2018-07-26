### What is it?

`ecs-tool` can run commands on an ECS cluster. There are some tools available, like official: [ecs-cli](https://github.com/aws/amazon-ecs-cli), `awscli`, and custom ones like [ecs-deploy](https://github.com/springload/ecs-deploy).

However, they have too many flags to just do one thing: run a command.
Also, those tools don't give the command output, and there are different flags or even tools to get it.

So what if there was a project- and environment-specific config with all the settings, so that you can concentrate only on a command you want to run?

### There it is

So that's it:

```
$ecs-tool run -h
Runs the specified command on an ECS cluster, optinally catching its output.

It can modify the container command.

Usage:
  ecs-tool run [flags]

Flags:
      --container_name string   Name of the container to modify parameters for
  -h, --help                    help for run
  -l, --log_group string        Name of the log group to get output

Global Flags:
  -c, --cluster string           name of cluster (required)
      --config string            config file to use. Overrides -e/--environment lookup
  -e, --environment string       look up config based on the environment flag. It looks for ecs-$environment.toml config in infra folder.
  -p, --profile string           name of profile to use
  -t, --task_definition string   name of task definition to use (required)
```

There are a couple of required flags, but they can be set either via enviromental variables or in the config.
It is possible to define a specific config by using `--config` flag, or rely on `ecs-tool` to look it up based on `--environment` flag.
The tool then will search for `infra/ecs-$environment.toml` file.

Just try running `ecs-tool envs` in a project folder to discover available environments.

It is as simple as this (while being in the project folder `/Users/user/company/project_name`):

```
$ecs-tool run -e production -- uptime
2018/07/26 09:52:24  info Using config file: /Users/user/company/project_name/infra/ecs-production.toml
2018/07/26 09:52:26  info waiting for the task to finish task_definition=project_name-production-app
 21:52:28 up 16 days, 10:45,  load average: 0.00, 0.02, 0.00
```

Even more, it is possible to configure `ecs-tool` via environmental variables instead of using config. Every flag has to be uppercased and prefixed by `ECS_`.
So that `--cluster` can be set by `ECS_CLUSTER` environmental variable, or `--task_definition` by `ECS_TASK_DEFINITION`.

Also, `ecs-tool` exit code is the same as the container exit code.

### AWS authorisation

It is handled by [aws-sdk-go](https://aws.amazon.com/sdk-for-go/) and supports all standard methods: env vars, `~/.aws/credential` and `~/.aws/config`.

### Installation

There are `deb` and `rpm` packages and binaries for those who don't use packages. Just head up to the releases page.
