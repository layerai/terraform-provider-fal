TEST?=$$(go list ./... | grep -v 'vendor')
HOSTNAME=fal-ai
NAMESPACE=iac
NAME=fal
BINARY=terraform-provider-${NAME}
VERSION=0.0.1
# Automatically detect OS and architecture
OS=$(shell go env GOOS)
ARCH=$(shell go env GOARCH)
OS_ARCH=$(OS)_$(ARCH)

default: install

build:
	go build -o ${BINARY}

release:
	goreleaser release --rm-dist --snapshot --skip-publish  --skip-sign

install: build
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}
	mv ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}

test:
	go test -i $(TEST) || exit 1
	echo $(TEST) | xargs -t -n4 go test $(TESTARGS) -timeout=30s -parallel=4

test:
	go test ./...

# Needs protoc, protoc-gen-go and protoc-gen-go-grpc on PATH.
generate-proto:
	protoc -I internal/isolate \
		--go_out=internal/isolate --go_opt=paths=source_relative \
		--go-grpc_out=internal/isolate --go-grpc_opt=paths=source_relative \
		internal/isolate/controller.proto

.PHONY: build release install test generate-proto
