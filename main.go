package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/ecs"
)

func main() {
	var EcsCluster string
	var TaskDefinition string
	var SecurityGroups string
	var Subnets string
	var LogGroup string
	var LaunchType string

	flag.StringVar(&EcsCluster, "cluster", "", "Name of the Cluster")
	flag.StringVar(&TaskDefinition, "task-definition", "", "Task Definition to use")
	flag.StringVar(&LogGroup, "log-group", "", "Log group used by ECS Task")
	flag.StringVar(&LaunchType, "launch-type", "FARGATE", "Launch Type: allowed EC2 or FARGATE")
	flag.StringVar(&SecurityGroups, "security-groups", "", "Security groups to use")
	flag.StringVar(&Subnets, "subnets", "", "subnets where to deploy task separated by comma")
	flag.Parse()

	//Get a slice form string input
	SGList := strings.Split(SecurityGroups, ",")
	SubnetList := strings.Split(Subnets, ",")

	//AWS session
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	AwsVpcConfiguration := ecs.AwsVpcConfiguration{
		SecurityGroups: aws.StringSlice(SGList),
		Subnets:        aws.StringSlice(SubnetList),
	}

	LogStreamName, TaskArnID := RunTask(sess, EcsCluster, LaunchType, TaskDefinition, AwsVpcConfiguration)

	GetLogs(sess, LogStreamName, LogGroup)
	exitCode, exitReason := GetExit(sess, EcsCluster, TaskArnID)
	fmt.Println("Exit reason:", exitReason)
	os.Exit(int(exitCode))
}

// RunTask runs task definition on specified ECS Cluster
// It returns the LogStreamName
func RunTask(sess *session.Session, Cluster string, LaunchType string, TaskDefinition string, AwsvpcConfiguration ecs.AwsVpcConfiguration) (string, string) {
	svc := ecs.New(sess)
	fmt.Printf("Launching task %s in an ECS Cluster %s...", TaskDefinition, Cluster)
	output, err := svc.RunTask(&ecs.RunTaskInput{
		Cluster:        aws.String(Cluster),
		Count:          aws.Int64(1),
		LaunchType:     aws.String(LaunchType),
		TaskDefinition: aws.String(TaskDefinition),
		NetworkConfiguration: &ecs.NetworkConfiguration{
			AwsvpcConfiguration: &AwsvpcConfiguration,
		},
	})
	if err != nil {
		fmt.Println("Got error launching task:")
		fmt.Println(err.Error())
		os.Exit(1)
	}

	TaskArn := *output.Tasks[0].TaskArn
	TaskArnSplit := strings.Split(TaskArn, "/")
	TaskArnID := TaskArnSplit[len(TaskArnSplit)-1]

	ContainerName := *output.Tasks[0].Containers[0].Name

	TaskDefinitionOutput, _ := svc.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(TaskDefinition),
	})
	logPrefix := *TaskDefinitionOutput.TaskDefinition.ContainerDefinitions[0].LogConfiguration.Options["awslogs-stream-prefix"]

	err = svc.WaitUntilTasksStopped(&ecs.DescribeTasksInput{
		Cluster: aws.String(Cluster),
		Tasks:   aws.StringSlice([]string{TaskArn}),
	})
	if err != nil {
		fmt.Println("Got error running the task:")
		fmt.Println(err.Error())
		os.Exit(1)
	}
	logGroupName := logPrefix + "/" + ContainerName + "/" + TaskArnID
	return logGroupName, TaskArnID
}

// GetLogs prints all the logs for specified LogStream sorted from earliest to latest.
func GetLogs(sess *session.Session, LogStreamName string, LogGroupName string) {
	svc := cloudwatchlogs.New(sess)

	resp, err := svc.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
		Limit:         aws.Int64(100),
		LogGroupName:  aws.String(LogGroupName),
		LogStreamName: aws.String(LogStreamName),
		StartFromHead: aws.Bool(true),
	})
	if err != nil {
		fmt.Println("Error getting log events:")
		fmt.Println(err.Error())
		os.Exit(1)
	}
	fmt.Println("Logs:")
	for _, event := range resp.Events {
		fmt.Println("  ", *event.Message)
	}
}

// GetExit Returns the exit code of the function and stoppedReason
func GetExit(sess *session.Session, ClusterName string, Task string) (int64, string) {
	svc := ecs.New(sess)
	output, err := svc.DescribeTasks(&ecs.DescribeTasksInput{
		Cluster: aws.String(ClusterName),
		Tasks:   aws.StringSlice([]string{Task}),
	})
	if err != nil {
		fmt.Println("Got error describing task:")
		fmt.Println(err.Error())
		os.Exit(1)
	}
	exitCode := *output.Tasks[0].Containers[0].ExitCode
	stoppedReason := *output.Tasks[0].StoppedReason
	return exitCode, stoppedReason
}
