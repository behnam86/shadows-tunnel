.PHONY: fmt test race vet build clean
fmt:
	gofmt -w .
test:
	go test ./...
race:
	go test -race ./...
vet:
	go vet ./...
build:
	mkdir -p bin
	go build -trimpath -ldflags='-s -w' -o bin/shadows ./cmd/shadows
clean:
	rm -rf bin dist
