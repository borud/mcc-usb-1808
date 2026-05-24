.PHONY: all
.PHONY: build
.PHONY: vet
.PHONY: clean

BINARY := synced_scan

all: vet build

build:
	@echo "*** build"
	@go build -trimpath -o $(BINARY) .

vet:
	@echo "*** vet"
	@go vet ./...

clean:
	@echo "*** clean"
	@rm -f $(BINARY)
