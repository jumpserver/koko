NAME=koko
BUILDDIR=build

BASEPATH := $(shell pwd)
BRANCH := $(shell git symbolic-ref HEAD 2>/dev/null | cut -d"/" -f 3)
BUILD := $(shell git rev-parse --short HEAD)
KOKOSRCFILE := $(BASEPATH)/cmd/koko/
KUBECTLFILE := $(BASEPATH)/cmd/kubectl/

VERSION ?= $(BRANCH)-$(BUILD)
BuildTime:= $(shell date -u '+%Y-%m-%d %I:%M:%S%p')
COMMIT:= $(shell git rev-parse HEAD)
GOVERSION:= $(shell go version)
CipherKey := $(shell head -c 100 /dev/urandom | base64 | head -c 32)

KOKOLDFLAGS+=-X 'main.Buildstamp=$(BuildTime)'
KOKOLDFLAGS+=-X 'main.Githash=$(COMMIT)'
KOKOLDFLAGS+=-X 'main.Goversion=$(GOVERSION)'
KOKOLDFLAGS+=-X 'github.com/jumpserver/koko/pkg/koko.Version=$(VERSION)'
KOKOLDFLAGS+=-X 'github.com/jumpserver/koko/pkg/config.CipherKey=$(CipherKey)'

KUBECTLFLAGS="-X 'github.com/jumpserver/koko/pkg/config.CipherKey=$(CipherKey)'"

KOKOBUILD=CGO_ENABLED=0 go build -trimpath -ldflags "$(KOKOLDFLAGS)"
KUBECTLBUILD=CGO_ENABLED=0 go build -trimpath -ldflags $(KUBECTLFLAGS)

PLATFORM_LIST = \
	darwin-amd64 \
	linux-amd64 \
	linux-arm64

all-arch: $(PLATFORM_LIST)

darwin-amd64:
	GOARCH=amd64 GOOS=darwin $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$@ $(KOKOSRCFILE)
	GOARCH=amd64 GOOS=darwin $(KUBECTLBUILD) -o $(BUILDDIR)/kubectl-$@ $(KUBECTLFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/static/
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/templates/
	cp $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/$(NAME)
	cp $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/kubectl
	cp -r $(BASEPATH)/locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	cp -r $(BASEPATH)/static/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/static/
	cp -r $(BASEPATH)/templates/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/templates/
	cp -r $(BASEPATH)/config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$@/config_example.yml
	cp -r $(BASEPATH)/utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$@/init-kubectl.sh

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$@.tar.gz $(NAME)-$(VERSION)-$@
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$@ $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/kubectl-$@

linux-amd64:
	GOARCH=amd64 GOOS=linux $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$@ $(KOKOSRCFILE)
	GOARCH=amd64 GOOS=linux $(KUBECTLBUILD) -o $(BUILDDIR)/kubectl-$@ $(KUBECTLFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/static/
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/templates/
	cp $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/$(NAME)
	cp $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/kubectl
	cp -r $(BASEPATH)/locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	cp -r $(BASEPATH)/static/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/static/
	cp -r $(BASEPATH)/templates/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/templates/
	cp -r $(BASEPATH)/config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$@/config_example.yml
	cp -r $(BASEPATH)/utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$@/init-kubectl.sh

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$@.tar.gz $(NAME)-$(VERSION)-$@
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$@ $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/kubectl-$@

linux-arm64:
	GOARCH=arm64 GOOS=linux $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$@ $(KOKOSRCFILE)
	GOARCH=arm64 GOOS=linux $(KUBECTLBUILD) -o $(BUILDDIR)/kubectl-$@ $(KUBECTLFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/static/
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/templates/
	cp $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/$(NAME)
	cp $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/kubectl
	cp -r $(BASEPATH)/locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	cp -r $(BASEPATH)/static/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/static/
	cp -r $(BASEPATH)/templates/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/templates/
	cp -r $(BASEPATH)/config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$@/config_example.yml
	cp -r $(BASEPATH)/utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$@/init-kubectl.sh

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$@.tar.gz $(NAME)-$(VERSION)-$@
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$@ $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/kubectl-$@

.PHONY: docker
docker:
	@echo "build docker images"
	docker build -t jumpserver/koko .

.PHONY: clean
clean:
	-rm -rf $(BUILDDIR)
