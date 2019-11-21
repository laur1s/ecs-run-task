# ecs-run-task
A CLI tool that allows to run a one-off ECS task

## Usage
1. Build the app with `go build`

2. `mv ecs-run task /usr/local/bin`

3. Use the command:
```
ecs-run-task --cluster myFargate --task-definition nginx --security-groups sg-xxx  --subnets subnet-a,subnet-b,subnet-c --log-group ecs-log-group
```
