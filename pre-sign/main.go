package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"log"
	"os"
	"time"
)

type FileMetaData struct {
	Filename string `json:"file_name"`
}

type Body struct {
	Url string `json:"url"`
	Key string `json:"key"`
}

func HandleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Initialize a session in us-west-2 that the SDK will use to load
	// credentials from the shared credentials file ~/.aws/credentials.
	sess := session.Must(session.NewSession())
	log.Printf(request.Body)
	key, err := parseResponseStringToTypedObject(request.Body)
	log.Println(key)

	if err != nil {
		return events.APIGatewayProxyResponse{}, err
	}

	// Create S3 service client
	svc := s3.New(sess)

	req, _ := svc.PutObjectRequest(&s3.PutObjectInput{
		Bucket: aws.String(os.Getenv("INPUT_BUCKET")),
		Key:    aws.String(key),
	})
	url, err := req.Presign(2 * time.Minute)

	if err != nil {
		return events.APIGatewayProxyResponse{}, err
	}

	// construct response body
	responseBody := &Body{
		Key: key,
		Url: url,
	}

	var jsonResponse []byte
	jsonResponse, err = json.Marshal(responseBody)

	if err != nil {
		return events.APIGatewayProxyResponse{}, err

	}

	return events.APIGatewayProxyResponse{Body: string(jsonResponse), StatusCode: 200}, err
}

func main() {
	lambda.Start(HandleRequest)
}

func parseResponseStringToTypedObject(responseString string) (string, error) {

	b := []byte(responseString)
	var resp FileMetaData
	err := json.Unmarshal(b, &resp)

	if err == nil {
		return resp.Filename, nil
	} else {
		log.Print(fmt.Sprintf("Could not unmarshall JSON string: [%s]", err.Error()))
		return err.Error(), err
	}
}
