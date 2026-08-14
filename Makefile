.PHONY: build test race vet lint run docker-prepare docker-up docker-verify

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -o dist/cyber-amber ./cmd/cyber-amber

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

run:
	go run ./cmd/cyber-amber -config config.yaml

docker-prepare:
	./deploy/docker/prepare.sh

docker-up:
	docker compose up -d --build

docker-verify:
	./deploy/docker/verify.sh
