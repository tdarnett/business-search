.PHONY: build clean deploy

build:
	env GOOS=linux go build -ldflags="-s -w" -o bin/pre-sign pre-sign/main.go
	env GOOS=linux go build -ldflags="-s -w" -o bin/transform-data transform-data/main.go
	env GOOS=linux go build -ldflags="-s -w" -o bin/generate-payment-intent generate-payment-intent/main.go
	env GOOS=linux go build -ldflags="-s -w" -o bin/webhook-handler webhook-handler/main.go

clean:
	rm -rf ./bin

deploy: clean build
	sls deploy --verbose
