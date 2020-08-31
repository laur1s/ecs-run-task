package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/ecs"
)

var ecsCluster string
var taskDefinition string
var taskDefinitionFile bool
var securityGroups string
var subnets string
var launchType string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ecs-run-task",
	Short: "A tool for running a task in an ECS cluster",
	Run: func(cmd *cobra.Command, args []string) {
		if ecsCluster == "" || taskDefinition == "" {
			cmd.Usage()
			os.Exit(1)
		}
		sess := session.Must(session.NewSessionWithOptions(session.Options{
			SharedConfigState: session.SharedConfigEnable,
		}))

		if taskDefinitionFile {
			taskDefinition = ParseTaskDefinition(sess, taskDefinition)
			fmt.Println("Succesfully uploaded: ", taskDefinition)
		}

		logGroupName, logStreamName, taskArnID := RunTask(sess, ecsCluster, launchType, taskDefinition)

		GetLogs(sess, logStreamName, logGroupName)
		exitCode, exitReason := GetExit(sess, ecsCluster, taskArnID)
		fmt.Println("Exit reason:", exitReason)
		os.Exit(int(exitCode))
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	//cobra.OnInitialize(initConfig)
	rootCmd.Flags().StringVarP(&ecsCluster, "cluster", "c", "", "Name of the Cluster")
	rootCmd.Flags().StringVarP(&taskDefinition, "task-definition", "t", "", "Task Definition to use can be a json file if used with -f flag")
	rootCmd.Flags().BoolVarP(&taskDefinitionFile, "file", "f", false, "Read task definition from File")
	rootCmd.Flags().StringVarP(&launchType, "launch-type", "l", "FARGATE", "Launch Type: allowed EC2 or FARGATE")
	rootCmd.Flags().StringVarP(&securityGroups, "security-groups", "", "", "Security groups to use")
	rootCmd.Flags().StringVarP(&subnets, "subnets", "", "", "subnets where to deploy task separated by comma")
}

// RunTask runs task definition on specified ECS Cluster
// It returns the LogStreamName
func RunTask(sess *session.Session, ecsCluster string, launchType string, taskDefinition string) (string, string, string) {
	svc := ecs.New(sess)
	fmt.Printf("Launching task %s in an ECS Cluster %s...", taskDefinition, ecsCluster)
	runTaskInput := &ecs.RunTaskInput{
		Cluster:        aws.String(ecsCluster),
		Count:          aws.Int64(1),
		LaunchType:     aws.String(launchType),
		TaskDefinition: aws.String(taskDefinition),
	}
	if subnets != "" || securityGroups != "" {
		fmt.Println("test")
		runTaskInput.NetworkConfiguration = &ecs.NetworkConfiguration{
			AwsvpcConfiguration: &ecs.AwsVpcConfiguration{
				Subnets:        aws.StringSlice(strings.Split(subnets, ",")),
				SecurityGroups: aws.StringSlice(strings.Split(securityGroups, ",")),
			},
		}
	}
	output, err := svc.RunTask(runTaskInput)
	if err != nil {
		fmt.Println("Got error launching task:")
		fmt.Println(err.Error())
		os.Exit(1)
	}

	taskArn := *output.Tasks[0].TaskArn
	taskArnSplit := strings.Split(taskArn, "/")
	taskArnID := taskArnSplit[len(taskArnSplit)-1]

	containerName := *output.Tasks[0].Containers[0].Name

	taskDefinitionOutput, _ := svc.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(taskDefinition),
	})
	logPrefix := *taskDefinitionOutput.TaskDefinition.ContainerDefinitions[0].LogConfiguration.Options["awslogs-stream-prefix"]
	err = svc.WaitUntilTasksStopped(&ecs.DescribeTasksInput{
		Cluster: aws.String(ecsCluster),
		Tasks:   aws.StringSlice([]string{taskArn}),
	})
	if err != nil {
		fmt.Println("Got error running the task:")
		fmt.Println(err.Error())
		os.Exit(1)
	}
	logStreamName := logPrefix + "/" + containerName + "/" + taskArnID
	logGroupName := *taskDefinitionOutput.TaskDefinition.ContainerDefinitions[0].LogConfiguration.Options["awslogs-group"]
	return logGroupName, logStreamName, taskArnID
}

// GetLogs prints all the logs for specified LogStream sorted from earliest to latest.
func GetLogs(sess *session.Session, logStreamName string, logGroupName string) {
	svc := cloudwatchlogs.New(sess)

	resp, err := svc.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
		Limit:         aws.Int64(100),
		LogGroupName:  aws.String(logGroupName),
		LogStreamName: aws.String(logStreamName),
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
func GetExit(sess *session.Session, ecsCluster string, task string) (int64, string) {
	svc := ecs.New(sess)
	output, err := svc.DescribeTasks(&ecs.DescribeTasksInput{
		Cluster: aws.String(ecsCluster),
		Tasks:   aws.StringSlice([]string{task}),
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

// Parses task
func ParseTaskDefinition(sess *session.Session, fileName string) string {
	svc := ecs.New(sess)
	var ecsTaskDefinition ecs.RegisterTaskDefinitionInput
	jsonFile, err := os.Open(fileName)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("Successfully Opened task definition:", fileName)
	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)
	json.Unmarshal(byteValue, &ecsTaskDefinition)
	output, err := svc.RegisterTaskDefinition(&ecsTaskDefinition)
	if err != nil {
		fmt.Println("Got error registering task definition:")
		fmt.Println(err.Error())
		os.Exit(1)
	}
	return *output.TaskDefinition.TaskDefinitionArn

}
