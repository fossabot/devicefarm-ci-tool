package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/artemnikitin/devicefarm-ci-tool/errors"
	"github.com/artemnikitin/devicefarm-ci-tool/model"
	"github.com/artemnikitin/devicefarm-ci-tool/service"
	"github.com/artemnikitin/devicefarm-ci-tool/tools"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/devicefarm"
	"github.com/aws/aws-sdk-go/service/devicefarm/devicefarmiface"
)

var (
	project    = flag.String("project", "", "Device Farm project name")
	runName    = flag.String("run", "", "Name of test run")
	appPath    = flag.String("app", "", "Path to an app")
	testPath   = flag.String("test", "", "Path to test app")
	devicePool = flag.String("devices", "Top Devices", "Specify list of devices for tests")
	configJSON = flag.String("config", "", "Path to JSON config")
	wait       = flag.Bool("wait", false, "Wait for run end")
	checkEvery = flag.Int("checkEvery", 5, "Specified time slice for checking status of run")
)

func main() {
	flag.Parse()

	if *project == "" || *appPath == "" {
		fmt.Println("Please specify correct parameters!")
		fmt.Println("You should specify:")
		fmt.Println("-project, name of a project in AWS Device Farm")
		fmt.Println("-app, path to an app you want to run")
		os.Exit(1)
	}

	failed, pass := runJob(getAWSClient(), getConfig())
	if !pass {
		if len(failed) == 0 {
			log.Println(fmt.Sprintf("Test job fails, it looks like AWS/infrastructure/setup issue, check the report."))
		} else {
			log.Println(fmt.Sprintf("There are %d test fails, check it out!\n", len(failed)))
			for i := 0; i < len(failed); i++ {
				fmt.Println(failed[i].ToString())
			}
		}
		os.Exit(1)
	}
}

func runJob(client devicefarmiface.DeviceFarmAPI, config *model.RunConfig) ([]*model.FailedTest, bool) {
	svc := &service.DeviceFarmRun{
		Client:  client,
		Config:  config,
		Project: *project,
	}

	if svc.GetProjectArn() == "" {
		log.Fatal("Application finished, because it can't retrieve project ARN")
	}

	svc.GetDevicePoolArn(svc.Config.DevicePoolName)

	appArn, url := svc.CreateUpload(*appPath)
	code := tools.UploadFile(*appPath, url)
	if code != 200 {
		log.Fatal("Can't upload an app to Device Farm")
	}

	svc.AppArn = appArn
	svc.WaitForAppProcessed(svc.AppArn, *checkEvery)

	runArn, status := svc.RunWithConfig()
	statusCheck(status)

	pass := true
	var failedTests []*model.FailedTest

	if *wait {
		result := svc.WaitForRunEnds(runArn, *checkEvery)
		if result != devicefarm.ExecutionResultPassed {
			failedTests = svc.GetListOfFailedTests(runArn)
			pass = false
		}
	}

	printReportURL(runArn)
	return failedTests, pass
}

func getConfig() *model.RunConfig {
	configFile := &model.RunConfig{}
	if *configJSON != "" {
		bytes, err := ioutil.ReadFile(*configJSON)
		errors.Validate(err, "Can't read model file")
		configFile = model.Transform(bytes)
	}
	if configFile.DevicePoolArn == "" && configFile.DevicePoolName == "" {
		configFile.DevicePoolName = *devicePool
	}
	if *runName != "" {
		configFile.Name = *runName
	}
	if *testPath != "" {
		configFile.TestPackagePath = *testPath
	}
	return configFile
}

func getAWSClient() devicefarmiface.DeviceFarmAPI {
	config := aws.NewConfig()
	config.WithCredentials(credentials.NewEnvCredentials())
	config.WithRegion("us-west-2")
	ses, err := session.NewSession(config)
	errors.Validate(err, "Can't create an AWS session")
	return devicefarm.New(ses)
}

func statusCheck(status string) {
	if status == devicefarm.ExecutionStatusScheduling {
		log.Println("Job is scheduled!")
	} else {
		log.Println("Status =", status)
		log.Fatal("Failed to start a job ...")
	}
}

func printReportURL(arn string) {
	log.Println("Test report URL:", tools.GenerateReportURL(arn))
}
