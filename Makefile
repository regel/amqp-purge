TAG = 0.0.1

IMAGE = regel/amqp-purge

test:
	GO111MODULE=on go test ./...

binary:
	CGO_ENABLED=0 GO111MODULE=on GOARCH=amd64 go build -trimpath -a -o build/_output/bin/purge main.go

build: binary
	docker build -f Dockerfile -t $(IMAGE):$(TAG) .

run-local: binary
	./build/_output/bin/purge

lint:
	golangci-lint run

.PHONY: build
