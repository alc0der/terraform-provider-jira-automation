HOSTNAME=registry.terraform.io
NAMESPACE=beno
NAME=jira-automation
BINARY=terraform-provider-${NAME}
VERSION=0.1.0
OS_ARCH=$(shell go env GOOS)_$(shell go env GOARCH)

default: install

build:
	go build -o ${BINARY}

install: build
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}
	cp ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}/

testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

.PHONY: build install testacc
