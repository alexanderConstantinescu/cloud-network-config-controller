build:
	CGO_ENABLED=0 GO111MODULE=on go build -mod vendor -o _output/bin/ ./...
test:
	go test ./... -count=1 -race
