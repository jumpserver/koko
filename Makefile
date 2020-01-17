BRANCH := $(shell git symbolic-ref HEAD 2>/dev/null | cut -d"/" -f 3)
BUILD := $(shell git rev-parse --short HEAD)
VERSION = $(BRANCH)-$(BUILD)
BASEPATH := $(shell pwd)

NAME := koko
SOFTWARENAME:=$(NAME)-$(VERSION)
BUILDDIR:=$(BASEPATH)/build
DIRNAME := kokodir
KOKOSRCFILE:= $(BASEPATH)/cmd/koko.go
VERSIONFLAGS="-X 'main.Buildstamp=`date -u '+%Y-%m-%d %I:%M:%S%p'`' -X 'main.Githash=`git rev-parse HEAD`' -X 'main.Goversion=`go version`'"
PLATFORMS := linux darwin

.PHONY: release
release: linux darwin Asset
	@echo "编译完成"
	rm -rf $(BUILDDIR)/$(DIRNAME)
	ls $(BUILDDIR)/koko*

.PHONY: Asset
Asset:
	@[ -d $(BUILDDIR) ] || mkdir -p $(BUILDDIR)
	@[ -d $(BUILDDIR)/$(DIRNAME) ] || mkdir -p $(BUILDDIR)/$(DIRNAME)
	cp -r $(BASEPATH)/cmd/locale $(BUILDDIR)/$(DIRNAME)
	cp -r $(BASEPATH)/cmd/static $(BUILDDIR)/$(DIRNAME)
	cp -r $(BASEPATH)/cmd/templates $(BUILDDIR)/$(DIRNAME)
	cp -r $(BASEPATH)/cmd/config_example.yml $(BUILDDIR)/$(DIRNAME)

.PHONY: $(PLATFORMS)
$(PLATFORMS): Asset
	@echo "编译" $@
	CGO_ENABLED=0 GOOS=$@ GOARCH=amd64 go build -ldflags $(VERSIONFLAGS) -x -o $(BUILDDIR)/$(NAME)-$@ $(KOKOSRCFILE)
	cp $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/$(DIRNAME)/$(NAME)
	tar czvf  $(BUILDDIR)/$(SOFTWARENAME)-$@-amd64.tar.gz -C $(BUILDDIR) $(DIRNAME)
	rm $(BUILDDIR)/$(NAME)-$@

.PHONY: docker
docker:
	@echo "build docker images"
	docker build -t koko --build-arg GOPROXY=$(GOPROXY) .

.PHONY: clean
clean:
	-rm -rf $(BUILDDIR)