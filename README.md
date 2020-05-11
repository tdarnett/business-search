# Business Search

This is the backend component of the business search tool, comprised of a collection of lambda functions written in Go. 
Together, they will allow a client to upload a CSV to s3, collect payment from the client, and receive a CSV with additional business data on each row via email... very complex.

1. `pre-sign` will take in a filename and generate a presigned PUT URL that gives the client permissions to write to an otherwise private s3 bucket.

2. `transform-data` will be triggered when a CSV is uploaded to the mentioned s3 bucket, parse the CSV and hit Google Place APIs to get more business address information. 
It will then construct a new CSV with the new data appended to the original CSV and upload this file to the s3 output bucket.

3. `generate-payment-intent` is triggered from a GET request containing the uploaded filename as a query parameter and return some basic pricing information along with a stripe payment intent,
if the csv conversion was successful. This will allow us to charge the charge the customer on the front end.

4. `webhook-handler` is triggered by webhooks from Stripe whenever a payment intent was successful or failed. 
Upon a success, we will email the CSV to the customer. If any failure occurs, we will email an admin with the purchase information.

The intention of this project is to help automate business development tasks for a friends company while experimenting with new software tools.
