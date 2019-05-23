
BRANCH := $(shell git symbolic-ref HEAD 2>/dev/null | cut -d"/" -f 3)
BUILD := $(shell git rev-parse --short HEAD)
VERSION=$(BRANCH)-$(BUILD)

NAME := coco
BASEPATH := $(shell pwd)
CGO_ENABLED = 0
GOCMD=go
GOBUILD=$(GOCMD) build

SOFTWARENAME=$(NAME)-$(VERSION)
COCOSRCFILE= coco.go

.PHONY: windows
windows:
	@echo "编译windows"
	mkdir -p $(BASEPATH)/../build
	GOOS=windows GOARCH=amd64 go build -o $(BASEPATH)/$(SOFTWARENAME)-windows-amd64 $(COCOSRCFILE)
	tar czvf $(BASEPATH)/../build/$(SOFTWARENAME)-windows-amd64.tar.gz $(SOFTWARENAME)-windows-amd64 locale/ config_example.yml

.PHONY: linux
linux:
	@echo "编译linux"
	mkdir -p $(BASEPATH)/../build
	GOOS=linux GOARCH=amd64 go build -o $(BASEPATH)/$(SOFTWARENAME)-linux-amd64 $(COCOSRCFILE)
	tar czvf $(BASEPATH)/../build/$(SOFTWARENAME)-linux-amd64.tar.gz $(SOFTWARENAME)-linux-amd64 locale/ config_example.yml

.PHONY: darwin
darwin:
	@echo "编译darwin"
	mkdir -p $(BASEPATH)/../build
	GOOS=darwin GOARCH=amd64 go build -o $(BASEPATH)/$(SOFTWARENAME)-darwin-amd64 $(COCOSRCFILE)
	tar czvf  $(BASEPATH)/../build/$(SOFTWARENAME)-darwin-amd64.tar.gz $(SOFTWARENAME)-darwin-amd64 locale/ config_example.yml

.PHONY: clean
clean:
	-rm -rf $(BASEPATH)/../build


