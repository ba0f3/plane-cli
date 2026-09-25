.PHONY: build test vet check clean

build:
	go build -trimpath -o bin/plane ./cmd/plane

test:
	go test ./...

vet:
	go vet ./...

check: test vet
	go build ./cmd/plane

clean:
	rm -rf bin
