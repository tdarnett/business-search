package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/paymentintent"
	"io"
	"log"
	"os"
)

type s3QueryResponse struct {
	Count int64 `json:"count"`
}

type Body struct {
	Count        int64  `json:"count"`
	Amount       int64  `json:"amount"`
	ClientSecret string `json:"client_secret"`
}

// Will query the csv on s3 and return the count of completed lines
// and return the count along with stripe payment intent
func attemptPaymentIntent(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {

	sess := session.Must(session.NewSession())

	bucket := os.Getenv("OUTPUT_BUCKET")
	var pricePerLine int64 = 10 // charge 10 cents per row

	uploadKey := request.QueryStringParameters["key"]
	key := "output-" + uploadKey
	log.Println(key)

	// Create S3 service client
	svc := s3.New(sess)

	sqlQuery := "SELECT COUNT(*) as \"count\" FROM S3Object WHERE Address IS NOT NULL AND Address <> ''"

	params := &s3.SelectObjectContentInput{
		Bucket:         aws.String(bucket),
		Key:            aws.String(key),
		ExpressionType: aws.String(s3.ExpressionTypeSql),
		Expression:     aws.String(sqlQuery),
		InputSerialization: &s3.InputSerialization{
			CSV: &s3.CSVInput{
				FileHeaderInfo: aws.String(s3.FileHeaderInfoUse),
			},
		},
		OutputSerialization: &s3.OutputSerialization{
			JSON: &s3.JSONOutput{},
		},
	}

	resp, err := svc.SelectObjectContent(params)
	if err != nil {
		return events.APIGatewayProxyResponse{}, err
	}

	defer resp.EventStream.Close()

	results, resultWriter := io.Pipe()
	go func() {
		defer resultWriter.Close()
		for event := range resp.EventStream.Events() {
			switch e := event.(type) {
			case *s3.RecordsEvent:
				resultWriter.Write(e.Payload)
			}
		}
	}()

	if err := resp.EventStream.Err(); err != nil {
		return events.APIGatewayProxyResponse{}, fmt.Errorf("failed to read from SelectObjectContent EventStream, %v", err)
	}

	// query the count from the result
	var s3Response s3QueryResponse

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(results)

	if err != nil {
		return events.APIGatewayProxyResponse{}, err
	}

	// deserialize json data
	err = json.Unmarshal(buf.Bytes(), &s3Response)
	if err != nil {
		return events.APIGatewayProxyResponse{}, err
	}

	count := s3Response.Count
	amountToCharge := count * pricePerLine

	paymentIntent, err := generatePaymentIntent(amountToCharge, key)
	if err != nil {
		return events.APIGatewayProxyResponse{}, err
	}

	// construct response body
	responseBody := &Body{
		Amount:       amountToCharge,
		Count:        count,
		ClientSecret: paymentIntent.ClientSecret,
	}

	var jsonResponse []byte
	jsonResponse, err = json.Marshal(responseBody)

	if err != nil {
		return events.APIGatewayProxyResponse{}, err

	}

	return events.APIGatewayProxyResponse{Body: string(jsonResponse), StatusCode: 200}, err

}

func main() {
	lambda.Start(attemptPaymentIntent)
}

func generatePaymentIntent(amount int64, key string) (*stripe.PaymentIntent, error) {
	// Get the STRIPE_API_KEY environment variable
	stripeAPIKey, exists := os.LookupEnv("STRIPE_API_KEY")

	if !exists {
		log.Fatal("No STRIPE_API_KEY key found!")
	}

	stripe.Key = stripeAPIKey

	stripeParams := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(amount),
		Currency: stripe.String(string(stripe.CurrencyCAD)),
	}
	// Verify your integration in this guide by including this parameter
	stripeParams.AddMetadata("filename", key)

	return paymentintent.New(stripeParams)
}
