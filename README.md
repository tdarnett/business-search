# Go Business Search

This is the backend component of the business search tool. It is comprised of two GO lambda functions: pre-sign and transform-data. 
Together, they will allow a client to upload a CSV to s3 and receive a CSV with additional business data on each row... very complex.


1. `pre-sign` will take in a filename and generate a presigned PUT URL that gives the client permissions to write to an otherwise private s3 bucket.

2. `transform-data` will be triggered when a CSV is uploaded to the mentioned s3 bucket, parse the CSV and hit Google Place APIs to get more business address information. 
It will then construct a new CSV with the new data appended to the original CSV and upload this file to the s3 output bucket.
