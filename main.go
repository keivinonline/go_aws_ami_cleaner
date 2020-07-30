package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

var (
	awsRegion = os.Getenv("AWS_REGION")
)

// HandleRequest handles lambda requests
func HandleRequest(ctx context.Context) (map[string]map[string]string, error) {

	// Create Lambda service client
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	// Create new EC2 client
	svc := ec2.New(sess)
	// get environment vars
	fmt.Println("## Environment Variables ##")
	fmt.Printf("'AWS_REGION' is set to [%v]\n", awsRegion)
	tagKey, tagValues, dryRun := getTags()
	amiAge := getAmiAge()

	// format inputs
	// debug
	fmt.Println("## AMI Filters ##")
	fmt.Printf("tagKey: [%v]\n", tagKey)
	input := formatInput(tagKey, tagValues, amiAge)
	ami, err := svc.DescribeImages(input)
	if err != nil {
		fmt.Println("there was an error listing instances in", err.Error())
		log.Fatal(err.Error())
	}
	// get AMI creation time
	creationDatesMap := getAmiAgeMap(ami)

	// get AMI to snapshots mapping
	snapshotsMap := getSnapshotMap(ami)

	// get finalSnapshots
	finalSnapshotMap := getFinalSnapshotMap(amiAge, creationDatesMap, snapshotsMap)

	// summary
	fmt.Println("## Summary ##")
	fmt.Printf("Total AMIs: %v\n", len(finalSnapshotMap))
	totalSnapshots := 0
	for _, v1 := range finalSnapshotMap {
		for range v1 {
			totalSnapshots++
		}
	}
	fmt.Printf("Total Snapshots: %v\n", totalSnapshots)

	// deregister image
	fmt.Println("## Running Deregister Jobs ##")
	deregisterAMI(svc, finalSnapshotMap, dryRun)
	return finalSnapshotMap, nil
}
func main() {
	lambda.Start(HandleRequest)

}

func deregisterAMI(svc *ec2.EC2, getFinalSnapshotMap map[string]map[string]string, dryRun bool) {

	for i := range getFinalSnapshotMap {
		// create inputs
		imageInput := &ec2.DeregisterImageInput{
			DryRun:  &dryRun,
			ImageId: aws.String(i),
		}
		// fmt.Println(imageInput)
		// deregister
		_, err := svc.DeregisterImage(imageInput)
		if err != nil {
			fmt.Printf("[%v] - '%v'\n", *imageInput.ImageId, err.Error())
			if strings.Contains(err.Error(), "DryRunOperation") {
				continue
			} else {
				log.Fatal(err.Error())
			}
		}
		// println()
		// fmt.Println(deregister)
	}

	return
}
func getFinalSnapshotMap(amiAge int, creationMap map[string]int, snapshotsMap map[string]map[string]string) (finalSnapshotMap map[string]map[string]string) {
	finalSnapshotMap = snapshotsMap
	for i, v := range creationMap {
		if v < amiAge {
			delete(finalSnapshotMap, i)
		}
	}
	return
}
func getAmiAgeMap(ami *ec2.DescribeImagesOutput) (creationMap map[string]int) {

	creationMap = map[string]int{}
	getTime := time.Now()

	for _, v1 := range ami.Images {

		creationTime, err := time.Parse(time.RFC3339, *v1.CreationDate)
		if err != nil {
			panic(err)
		}
		daysDiff := int(getTime.Sub(creationTime).Hours() / 24)
		creationMap[*v1.ImageId] = daysDiff
	}

	return
}

// getSnapshotMap gets all AMI Ids and respective snapshot Ids
func getSnapshotMap(ami *ec2.DescribeImagesOutput) (snapshotsMap map[string]map[string]string) {
	snapshotsMap = map[string]map[string]string{}

	for _, v1 := range ami.Images {
		snapshotsMap[*v1.ImageId] = map[string]string{}
		for _, v2 := range v1.BlockDeviceMappings {
			snapshotsMap[*v1.ImageId][*v2.DeviceName] = *v2.Ebs.SnapshotId
		}
	}
	return
}

func getAmiAge() (amiAge int) {

	amiAge, err := strconv.Atoi(os.Getenv("AMI_AGE"))
	if err != nil {
		fmt.Println("'AMI_AGE' is not set. Default value of '14' is used")
		amiAge = 14
	} else {

		fmt.Printf("'AMI_AGE' value of '%d' is set\n", amiAge)
	}
	println()
	return
}
func getTags() (tagKey, tagValues string, dryRun bool) {

	tagKey = os.Getenv("TAG_KEY")
	if len(tagKey) == 0 {
		fmt.Println("'TAG_KEY' is not set. Default value of 'name' is used")
		tagKey = "name"
	} else {
		fmt.Printf("'TAG_KEY' of [%v] is set\n", tagKey)
	}
	tagValues = os.Getenv("TAG_VALUES")
	if len(tagValues) == 0 {
		fmt.Println("'TAG_VALUES' is not set. Default value of 'windows2016-base' is used")
		tagValues = "windows2016-base"
	} else {
		fmt.Printf("'TAG_VALUES' of [%v] is set\n", tagValues)
	}

	dryRun, err := strconv.ParseBool(os.Getenv("DRY_RUN"))
	if err != nil {
		log.Fatal("'DRY_RUN' value is invalid. Valid entries are 'true' or 'false'\n", err.Error())
	} else {
		fmt.Printf("'DRY_RUN' of [%v] is set\n", dryRun)
	}

	return
}

// formatInput formats based on the tag key and values
func formatInput(tagKey, tagValues string, amiAge int) (input *ec2.DescribeImagesInput) {
	// format environment variables
	tagKey = "tag:" + strings.TrimSpace(tagKey)
	tagValueSlice := []string{}
	for _, v := range strings.Split(tagValues, ";") {
		tagValueSlice = append(tagValueSlice, strings.TrimSpace(v)+"*")
	}
	for i, v := range tagValueSlice {
		fmt.Printf("tagValues[%d]:[%v]\n", i, v)
	}
	fmt.Printf("amiAge: [%d]\n\n", amiAge)
	// format inputs
	input = &ec2.DescribeImagesInput{
		Owners: []*string{
			aws.String("self"),
		}, Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   aws.String(tagKey),
				Values: aws.StringSlice(tagValueSlice),
			},
		},
	}
	return
}
