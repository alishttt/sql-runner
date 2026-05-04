.PHONY: deps run test build clean tidy

deps:
	go mod tidy

run:
	go run ./...

test:
	go test -v -race ./...

build:
	go build -o bin/sql-runner ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/
