BINARIES := $(notdir $(shell find cmd -mindepth 1 -maxdepth 1 -type d))
VERSION  := $(shell ver version 2>/dev/null || echo dev)
BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS  := -ldflags "-X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE)"

.PHONY: $(BINARIES)
.PHONY: all
.PHONY: build
.PHONY: run
.PHONY: test
.PHONY: fuzz
.PHONY: bench
.PHONY: benchstat
.PHONY: vet
.PHONY: lint
.PHONY: revive
.PHONY: staticcheck
.PHONY: gosec
.PHONY: fmt
.PHONY: tidy
.PHONY: clean
.PHONY: linux-amd64
.PHONY: linux-arm64
.PHONY: linux

all: lint vet staticcheck gosec test build

build: $(BINARIES)

$(BINARIES):
	@echo "*** $@"
	@cd cmd/$@ && go build $(LDFLAGS) -trimpath -o ../../bin/$@

run:
	@echo "*** $@"
	@go run cmd/

test:
	@echo "*** $@"
	@CGO_LDFLAGS="-Wl,-no_warn_duplicate_libraries" go test ./...

bench:
	@echo "*** $@"
	@go test -bench=. -benchmem -count=6 ./... | tee bench/new.txt

benchstat:
	@echo "*** $@"
	@benchstat bench/old.txt bench/new.txt

vet:
	@echo "*** $@"
	@go vet ./...

lint:
	@echo "*** $@"
	@revive ./...

staticcheck:
	@staticcheck ./...

gosec:
	@echo "*** $@"
	@gosec -quiet -exclude=G104,G115 ./...

FUZZ_TIME ?= 1h

fuzz:
	@echo "*** $@ ($(FUZZ_TIME) per target)"
	@for target in FuzzScanBatchSize FuzzScanReadTimeout FuzzScanParamsEndToEnd; do \
		echo "--- $$target"; \
		CGO_LDFLAGS="-Wl,-no_warn_duplicate_libraries" go test -fuzz $$target -fuzztime $(FUZZ_TIME) . || exit 1; \
	done
	@for target in FuzzCaptureRaw FuzzCalibrate FuzzCaptureSeekable; do \
		echo "--- $$target"; \
		go test -fuzz $$target -fuzztime $(FUZZ_TIME) ./capture || exit 1; \
	done


fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -rf bin

# Cross-compilation using podman
CONTAINER_ENGINE ?= podman
IMAGE_NAME       := mcc-usb-1808-builder
GO_VERSION       := $(shell grep '^go ' go.mod | awk '{print $$2}')

linux-amd64:
	@echo "*** $@"
	@mkdir -p bin
	@$(CONTAINER_ENGINE) build \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg TARGETARCH=amd64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		--output type=local,dest=bin/ \
		--tag $(IMAGE_NAME):amd64 \
		--ignorefile build/.containerignore \
		-f build/Containerfile .
	@mv bin/daq bin/daq-linux-amd64

linux-arm64:
	@echo "*** $@"
	@mkdir -p bin
	@$(CONTAINER_ENGINE) build \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg TARGETARCH=arm64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		--output type=local,dest=bin/ \
		--tag $(IMAGE_NAME):arm64 \
		--ignorefile build/.containerignore \
		-f build/Containerfile .
	@mv bin/daq bin/daq-linux-arm64

linux: linux-amd64 linux-arm64
