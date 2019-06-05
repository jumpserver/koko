
BRANCH := $(shell git symbolic-ref HEAD 2>/dev/null | cut -d"/" -f 3)
BUILD := $(shell git rev-parse --short HEAD)
VERSION = $(BRANCH)-$(BUILD)

NAME := coco
BASEPATH := $(shell pwd)
CGO_ENABLED = 0
GOCMD = go
GOBUILD = $(GOCMD) build

ASSETS = $(shell echo "locale static templates coco config_example.yml")
SOFTWARENAME = $(NAME)-$(VERSION)
COCOSRCFILE = coco.go


.PHONY: windows
windows:
	@echo "编译windows"
	mkdir -p $(BASEPATH)/../build
	GOOS=windows GOARCH=amd64 go build -o $(BASEPATH)/$(NAME) $(COCOSRCFILE)
	tar czvf $(BASEPATH)/../build/$(SOFTWARENAME)-windows-amd64.tar.gz $(SOFTWARENAME)-windows-amd64 locale/ config_example.yml

.PHONY: linux
linux:
	@echo "编译linux"
	mkdir -p $(BASEPATH)/../build
	GOOS=linux GOARCH=amd64 go build -o $(BASEPATH)/$(NAME) $(COCOSRCFILE)
	tar czvf $(BASEPATH)/../build/$(SOFTWARENAME)-linux-amd64.tar.gz $(ASSETS)

.PHONY: darwin
darwin:
	@echo "编译darwin"
	mkdir -p $(BASEPATH)/../build
	GOOS=darwin GOARCH=amd64 go build -o $(BASEPATH)/$(NAME) $(COCOSRCFILE)
	tar czvf  $(BASEPATH)/../build/$(SOFTWARENAME)-darwin-amd64.tar.gz $(SOFTWARENAME)-darwin-amd64 locale/ config_example.yml

.PHONY: docker
docker:
	@echo "build docker images"
	docker build -t cocogo --build-arg http_proxy=$(http_proxy) --build-arg https_proxy=$(https_proxy) $(BASEPATH)/../

.PHONY: clean
clean:
	-rm -rf $(BASEPATH)/../build


