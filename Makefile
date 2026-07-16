.PHONY: all build-linux clean test

PLUGIN_NAME := commandcode
GOOS := linux
GOARCH := amd64

all: build-linux

build-linux:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -buildmode=c-shared -o $(PLUGIN_NAME).so .

clean:
	rm -f $(PLUGIN_NAME).so $(PLUGIN_NAME).h

test:
	go test ./...
