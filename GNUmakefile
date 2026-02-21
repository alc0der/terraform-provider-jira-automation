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

test:
	go test ./... -short -v

testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

lint-env:
	dotenv-linter check --ignore-checks UnorderedKey .env.example
	@if [ -f .env ]; then dotenv-linter check --schema .env.schema --ignore-checks UnorderedKey .env && dotenv-linter diff .env.example .env; fi

dev.tfrc: build
	@printf 'provider_installation {\n  dev_overrides {\n    "registry.terraform.io/beno/jira-automation" = "%s"\n  }\n  direct {}\n}\n' "$(CURDIR)" > dev.tfrc

.PHONY: build install test testacc lint-env
