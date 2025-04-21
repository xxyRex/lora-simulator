.PHONY: build clean
VERSION := $(shell git describe --always |sed -e "s/^v//")

build:
	@echo "Compiling source"
	@mkdir -p build
	go build $(GO_EXTRA_BUILD_ARGS) -ldflags "-s -w -X main.version=$(VERSION)" -o cmd/lora-simulator/lora-simulator cmd/lora-simulator/main.go

clean:
	@echo "Cleaning up workspace"
	@rm -rf cmd/lora-simulator/lora-simulator
	@rm -rf dist
	@rm -rf docs/public
