BRANCH := $(shell git symbolic-ref HEAD 2>/dev/null | cut -d"/" -f 3)
BUILD := $(shell git rev-parse --short HEAD)
VERSION = $(BRANCH)-$(BUILD)
BASEPATH := $(shell pwd)

NAME := koko
SOFTWARENAME:=$(NAME)-$(VERSION)
BUILDDIR:=$(BASEPATH)/build
DIRNAME := kokodir
KOKOSRCFILE:= $(BASEPATH)/cmd/koko/
KUBECTLFILE:= $(BASEPATH)/cmd/kubectl/
BuildTime:= $(shell date -u '+%Y-%m-%d %I:%M:%S%p')
GitHash:= $(shell git rev-parse HEAD)
GoVersion:= $(shell go version)
CipherKey := $(shell head -c 100 /dev/urandom | base64 | head -c 32)
KOKOFLAGS="-X 'main.Buildstamp=$(BuildTime)' -X 'main.Githash=$(GitHash)' -X 'main.Goversion=$(GoVersion)' -X 'github.com/jumpserver/koko/pkg/config.CipherKey=$(CipherKey)'"
KUBECTLFLAGS="-X 'github.com/jumpserver/koko/pkg/config.CipherKey=$(CipherKey)'"
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
	cp -r $(BASEPATH)/locale $(BUILDDIR)/$(DIRNAME)
	cp -r $(BASEPATH)/static $(BUILDDIR)/$(DIRNAME)
	cp -r $(BASEPATH)/templates $(BUILDDIR)/$(DIRNAME)
	cp -r $(BASEPATH)/config_example.yml $(BUILDDIR)/$(DIRNAME)
	cp -r $(BASEPATH)/utils/init-kubectl.sh $(BUILDDIR)/$(DIRNAME)
	cp -r $(BASEPATH)/utils/coredump.sh $(BUILDDIR)/$(DIRNAME)

.PHONY: $(PLATFORMS)
$(PLATFORMS): Asset
	@echo "编译" $@
	CGO_ENABLED=0 GOOS=$@ GOARCH=amd64 go build -ldflags $(KOKOFLAGS) -x -o $(BUILDDIR)/$(NAME)-$@ $(KOKOSRCFILE)
	CGO_ENABLED=0 GOOS=$@ GOARCH=amd64 go build -ldflags $(KUBECTLFLAGS) -x -o $(BUILDDIR)/kubectl-$@ $(KUBECTLFILE)
	cp $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/$(DIRNAME)/$(NAME)
	cp $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/$(DIRNAME)/kubectl
	tar czvf  $(BUILDDIR)/$(SOFTWARENAME)-$@-amd64.tar.gz -C $(BUILDDIR) $(DIRNAME)
	rm $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/kubectl-$@
	ls $(BUILDDIR)/$(SOFTWARENAME)*

.PHONY: docker
docker:
	@echo "build docker images"
	docker build -t koko .

.PHONY: clean
clean:
	-rm -rf $(BUILDDIR)