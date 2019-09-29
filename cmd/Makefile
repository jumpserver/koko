
BRANCH := $(shell git symbolic-ref HEAD 2>/dev/null | cut -d"/" -f 3)
BUILD := $(shell git rev-parse --short HEAD)
VERSION = $(BRANCH)-$(BUILD)

NAME := koko
DIRNAME := kokodir
BASEPATH := $(shell pwd)
CGO_ENABLED = 0
GOCMD = go
GOBUILD = $(GOCMD) build

SOFTWARENAME=$(NAME)-$(VERSION)
KOKOSRCFILE= koko.go
BUILDDIR:=$(BASEPATH)/../build
ASSETS=locale static templates config_example.yml
GOFLAGS="-X 'main.Buildstamp=`date -u '+%Y-%m-%d %I:%M:%S%p'`' -X 'main.Githash=`git rev-parse HEAD`' -X 'main.Goversion=`go version`'"
PLATFORMS := linux darwin

.PHONY: release
release: linux darwin


.PHONY:Asset
Asset:
	@[ -d $(BUILDDIR) ] || mkdir -p $(BUILDDIR)
	@[ -d $(DIRNAME) ] || mkdir -p $(DIRNAME)
	cp -r $(ASSETS) $(DIRNAME)

.PHONY: $(PLATFORMS)
$(PLATFORMS): Asset
	@echo "编译" $@
	GOOS=$@ GOARCH=amd64 go build -ldflags $(GOFLAGS) -x -o $(NAME) $(KOKOSRCFILE)
	cp -f $(NAME) $(DIRNAME)
	tar czvf $(BUILDDIR)/$(SOFTWARENAME)-$@-amd64.tar.gz $(DIRNAME)

.PHONY: docker
docker:
	@echo "build docker images"
	docker build -t koko --build-arg GOPROXY=$(GOPROXY) $(BASEPATH)/../

.PHONY: clean
clean:
	-rm -rf $(NAME)
	-rm -rf $(DIRNAME)
	-rm -rf $(BUILDDIR)


