package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"googlemaps.github.io/maps"
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

type Business struct {
	BusinessName     string `json:"businessName"`
	City             string `json:"city"`
	Province         string `json:"province"`
	PlaceID          string `json:"placeID"`
	PhoneNumber      string `json:"phoneNumber"`
	FormattedAddress string `json:"formattedAddress"`
	Website          string `json:"website"`
}

func check(err error) {
	if err != nil {
		log.Fatalf("fatal error: %s", err)
	}
}

func transformData(ctx context.Context, s3Event events.S3Event) (string, error) {
	var outputUrl string
	for _, record := range s3Event.Records {

		// The session the S3 Downloader will use
		sess := session.Must(session.NewSession())
		key := record.S3.Object.Key
		bucketName := record.S3.Bucket.Name

		fmt.Printf("recieved input file %q", key)
		var err error

		// Create a downloader with the session and default options
		downloader := s3manager.NewDownloader(sess)

		buff := &aws.WriteAtBuffer{} // download csv to buffer
		_, err = downloader.Download(buff,
			&s3.GetObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
			})

		if err != nil {
			log.Fatalf("Unable to download item %q, %v", key, err)
		}
		data := buff.Bytes()

		businessData := *parseData(data)

		// Get the GOOGLE_PLACES_API_KEY environment variable
		apiKey, exists := os.LookupEnv("GOOGLE_PLACES_API_KEY")

		if !exists {
			log.Fatal("No GOOGLE_PLACES_API_KEY key found!")
		}

		// initialize google maps client
		var client *maps.Client
		client, err = maps.NewClient(maps.WithAPIKey(apiKey))
		check(err)

		// s is a session token. Thread this through a series of requests that comprise
		// a single autocomplete search session.
		s := maps.NewPlaceAutocompleteSessionToken()

		var wg sync.WaitGroup

		for i := 0; i < len(businessData); i++ {
			wg.Add(1) // use goroutines to speed up http request process
			go populateBusiness(&businessData[i], &wg, s, client)
		}
		wg.Wait()

		// write to CSV
		outputUrl = uploadData(&businessData, sess, key)
	}

	return fmt.Sprintf(outputUrl), nil

}

func main() {
	lambda.Start(transformData)
}

func populateBusiness(business *Business, wg *sync.WaitGroup, s maps.PlaceAutocompleteSessionToken, client *maps.Client) {
	defer (*wg).Done()
	inputSlice := []string{business.BusinessName, business.City, business.Province}
	input := strings.Join(inputSlice, "")

	request := &maps.PlaceAutocompleteRequest{
		Input:        input,
		SessionToken: s,
	}

	resp, err := client.PlaceAutocomplete(context.Background(), request)
	check(err)

	if len(resp.Predictions) <= 0 {
		return
	}
	placeID := resp.Predictions[0].PlaceID
	// set on struct for possible later use
	business.PlaceID = placeID

	// look up place details
	detailsRequest := &maps.PlaceDetailsRequest{
		PlaceID: placeID,
	}

	response, err := client.PlaceDetails(context.Background(), detailsRequest)
	check(err)

	business.FormattedAddress = response.FormattedAddress
	business.PhoneNumber = response.InternationalPhoneNumber
	business.Website = response.Website
}

func uploadData(businesses *[]Business, sess *session.Session, key string) string {

	destBucketName := os.Getenv("OUTPUT_BUCKET")
	destKey := "output-" + key

	output := &bytes.Buffer{}
	csvwriter := csv.NewWriter(output)

	// write headers
	headerData := []string{"Business Name", "City", "Province", "Address", "Phone Number", "Website"}
	_ = csvwriter.Write(headerData)

	for _, business := range *businesses {
		businessData := []string{business.BusinessName, business.City, business.Province, business.FormattedAddress, business.PhoneNumber, business.Website}
		_ = csvwriter.Write(businessData)
	}
	csvwriter.Flush()

	upParams := &s3manager.UploadInput{
		Bucket: &destBucketName,
		Key:    &destKey,
		Body:   output,
	}

	// must upload to another bucket or we will enter an infinite loop of triggers
	uploader := s3manager.NewUploader(sess)
	result, err := uploader.Upload(upParams)
	check(err)

	return result.Location
}

func parseData(buffer []byte) *[]Business {
	reader := csv.NewReader(bytes.NewReader(buffer))

	var businesses []Business
	for {
		line, error := reader.Read()
		if error == io.EOF {
			break
		} else if error != nil {
			log.Fatal(error)
		}
		businesses = append(businesses, Business{
			BusinessName: strings.TrimSpace(line[0]),
			City:         strings.TrimSpace(line[1]),
			Province:     strings.TrimSpace(line[2]),
		})
	}
	return &businesses
}
