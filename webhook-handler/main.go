package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/ses"
	"github.com/stripe/stripe-go"
	"gopkg.in/gomail.v2"
	"io"
	"io/ioutil"
	"os"
	"strconv"
)

const (
	// The subject line for the email.
	Subject = "Your completed file from BusinessSearch!"

	// The HTML body for the email.
	HtmlBody = "<h1>Thank you for using BusinessSearch!</h1>" +
		"<p>Your file is attached and a receipt has been sent.</p>"
)

func webhookHandler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {

	payload := request.Body

	event := stripe.Event{}

	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return acknowledgment(fmt.Sprintf("Failed to parse webhook body json: %v\\n", err.Error()))
	}

	// Unmarshal the event data into an appropriate struct depending on its Type
	switch event.Type {
	case "payment_intent.succeeded":
		var paymentIntent stripe.PaymentIntent
		err := json.Unmarshal(event.Data.Raw, &paymentIntent)
		if err != nil {
			return acknowledgment(fmt.Sprintf("Error parsing webhook JSON: %v\\n", err))
		}

		paymentIntent.Metadata["filename"] = "cleanedInput.csv" // TODO remove after tesing

		recipientEmail := paymentIntent.ReceiptEmail
		fileName := paymentIntent.Metadata["filename"]

		csvData, err := downloadCSV("cleanedInput.csv")

		if err != nil {
			return acknowledgment(fmt.Sprintf("Error constructing email! %v\n", err))
		}

		err = sendSuccessEmail(recipientEmail, csvData, fileName)
		if err != nil {
			return acknowledgment(fmt.Sprintf("Error sending email: %v\n", err))
		}

	case "payment_intent.payment_failed":
		var paymentIntent stripe.PaymentIntent
		err := json.Unmarshal(event.Data.Raw, &paymentIntent)
		if err != nil {
			return acknowledgment(fmt.Sprintf("Error parsing webhook JSON: %v\n", err))
		}

		err = sendFailureEmail(&paymentIntent)
		if err != nil {
			return acknowledgment(fmt.Sprintf("Error sending email: %v\n", err))
		}

	default:
		return acknowledgment(fmt.Sprintf("Unexpected event type: %s\n", event.Type))
	}

	return acknowledgment("success!")
}

func main() {
	lambda.Start(webhookHandler)
}

func acknowledgment(body string) (events.APIGatewayProxyResponse, error) {
	// Return a short message that will be visible in the stripe dashboard.
	// We must always respond with a 200 status so Stripe doesn't keep retrying
	result := events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       body,
	}
	return result, nil
}

func constructEmail(emailAddress string, subject string, body string) *gomail.Message {
	// construct email using gomail's helpful WriteTo function for SES requirements
	msg := gomail.NewMessage()
	msg.SetHeader("From", os.Getenv("FROM_EMAIL")) // This address must be verified with Amazon SES.
	msg.SetHeader("To", emailAddress)
	msg.SetHeader("Subject", subject)
	msg.SetBody("text/html", body)

	return msg
}

func sendSuccessEmail(emailAddress string, data []byte, filename string) error {
	msg := constructEmail(emailAddress, Subject, HtmlBody)

	// attach the csv
	msg.Attach(filename, gomail.SetCopyFunc(func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	}))

	// send a copy to admin as well
	msg.SetHeader("Bcc", os.Getenv("ADMIN_EMAIL"))

	err := sendEmail(msg)

	return err
}

func sendFailureEmail(intent *stripe.PaymentIntent) error {
	price := strconv.FormatInt(intent.Amount/int64(100), 10)
	subject := fmt.Sprintf("Payment for $%s %s has failed!", price, intent.Currency)
	body := fmt.Sprintf("Payment attempt of $%s %s from <%s> has failed. Status: %s", price, intent.Currency, intent.ReceiptEmail, intent.Status)

	msg := constructEmail(os.Getenv("ADMIN_EMAIL"), subject, body) // send error emails to sender email
	err := sendEmail(msg)

	return err
}

func sendEmail(email *gomail.Message) error {

	var emailRaw bytes.Buffer
	_, err := email.WriteTo(&emailRaw)
	if err != nil {
		return err
	}

	// create AWS SES session
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-west-2")},
	)

	svc := ses.New(sess)
	input := &ses.SendRawEmailInput{
		RawMessage: &ses.RawMessage{
			Data: emailRaw.Bytes(), // pass in the raw email data
		},
	}

	_, err = svc.SendRawEmail(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case ses.ErrCodeMessageRejected:
				fmt.Println(ses.ErrCodeMessageRejected, aerr.Error())
				return aerr
			case ses.ErrCodeMailFromDomainNotVerifiedException:
				fmt.Println(ses.ErrCodeMailFromDomainNotVerifiedException, aerr.Error())
				return aerr
			case ses.ErrCodeConfigurationSetDoesNotExistException:
				fmt.Println(ses.ErrCodeConfigurationSetDoesNotExistException, aerr.Error())
				return aerr
			case ses.ErrCodeConfigurationSetSendingPausedException:
				fmt.Println(ses.ErrCodeConfigurationSetSendingPausedException, aerr.Error())
				return aerr
			case ses.ErrCodeAccountSendingPausedException:
				fmt.Println(ses.ErrCodeAccountSendingPausedException, aerr.Error())
				return aerr
			default:
				fmt.Println(aerr.Error())
				return aerr
			}
		} else {
			// Print the error, cast err to awserr.Error to get the Code and
			// Message from an error.
			fmt.Println(err.Error())
			return aerr
		}
	}

	return nil
}

func downloadCSV(filename string) ([]byte, error) {
	sess := session.Must(session.NewSession())

	key := "output-" + filename

	// Create S3 service client
	svc := s3.New(sess)

	sqlQuery := "SELECT * FROM S3Object"

	params := &s3.SelectObjectContentInput{
		Bucket:         aws.String(os.Getenv("OUTPUT_BUCKET")),
		Key:            aws.String(key),
		ExpressionType: aws.String(s3.ExpressionTypeSql),
		Expression:     aws.String(sqlQuery),
		InputSerialization: &s3.InputSerialization{
			CSV: &s3.CSVInput{
				FileHeaderInfo: aws.String(s3.FileHeaderInfoUse),
			},
		},
		OutputSerialization: &s3.OutputSerialization{
			CSV: &s3.CSVOutput{},
		},
	}

	resp, err := svc.SelectObjectContent(params)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("failed to read from SelectObjectContent EventStream, %v", err)
	}

	// Convert to CSV file
	return ioutil.ReadAll(results)
}
