NAME=koko
BUILDDIR=build

BASEPATH := $(shell pwd)
BRANCH := $(shell git symbolic-ref HEAD 2>/dev/null | cut -d"/" -f 3)
BUILD := $(shell git rev-parse --short HEAD)
KOKOSRCFILE := $(BASEPATH)/cmd/koko/
KUBECTLFILE := $(BASEPATH)/cmd/kubectl/
HELMFILE := $(BASEPATH)/cmd/helm/

VERSION ?= $(BRANCH)-$(BUILD)
BuildTime:= $(shell date -u '+%Y-%m-%d %I:%M:%S%p')
COMMIT:= $(shell git rev-parse HEAD)
GOVERSION:= $(shell go version)
CipherKey := $(shell head -c 100 /dev/urandom | base64 | head -c 32)
TARGETARCH ?= amd64

UIDIR=ui
NPMINSTALL=yarn install
NPMBUILD=yarn build --no-clean

LDFLAGS=-w -s

KOKOLDFLAGS+=-X 'main.Buildstamp=$(BuildTime)'
KOKOLDFLAGS+=-X 'main.Githash=$(COMMIT)'
KOKOLDFLAGS+=-X 'main.Goversion=$(GOVERSION)'
KOKOLDFLAGS+=-X 'main.Version=$(VERSION)'
KOKOLDFLAGS+=-X 'github.com/jumpserver/koko/pkg/config.CipherKey=$(CipherKey)'

K8SCMDFLAGS=-X 'github.com/jumpserver/koko/pkg/config.CipherKey=$(CipherKey)'

KOKOBUILD=CGO_ENABLED=0 go build -trimpath -ldflags "$(KOKOLDFLAGS) ${LDFLAGS}"
K8SCMDBUILD=CGO_ENABLED=0 go build -trimpath -ldflags "$(K8SCMDFLAGS) ${LDFLAGS}"

CURRENT_OS_ARCH = $(shell go env GOOS)-$(shell go env GOARCH)

define make_artifact_full
	GOOS=$(1) GOARCH=$(2) $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$(1)-$(2) $(KOKOSRCFILE)
	GOOS=$(1) GOARCH=$(2) $(K8SCMDBUILD) -o $(BUILDDIR)/kubectl-$(1)-$(2) $(KUBECTLFILE)
	GOOS=$(1) GOARCH=$(2) $(K8SCMDBUILD) -o $(BUILDDIR)/helm-$(1)-$(2) $(HELMFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/locale/

	cp $(BUILDDIR)/$(NAME)-$(1)-$(2) $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/$(NAME)
	cp $(BUILDDIR)/kubectl-$(1)-$(2) $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/kubectl
	cp $(BUILDDIR)/helm-$(1)-$(2) $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/helm
	cp -r $(BASEPATH)/locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/locale/
	cp -r $(BASEPATH)/config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/config_example.yml
	cp -r $(BASEPATH)/utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/init-kubectl.sh

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$(1)-$(2).tar.gz $(NAME)-$(VERSION)-$(1)-$(2)
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2) $(BUILDDIR)/$(NAME)-$(1)-$(2) $(BUILDDIR)/kubectl-$(1)-$(2) $(BUILDDIR)/helm-$(1)-$(2)
endef

build:
	$(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$(CURRENT_OS_ARCH) $(KOKOSRCFILE)
	$(K8SCMDBUILD) -o $(BUILDDIR)/kubectl-$(CURRENT_OS_ARCH) $(KUBECTLFILE)
	$(K8SCMDBUILD) -o $(BUILDDIR)/helm-$(CURRENT_OS_ARCH) $(HELMFILE)

all: koko-ui
	$(call make_artifact_full,darwin,amd64)
	$(call make_artifact_full,darwin,arm64)
	$(call make_artifact_full,linux,amd64)
	$(call make_artifact_full,linux,arm64)
	$(call make_artifact_full,linux,mips64le)
	$(call make_artifact_full,linux,ppc64le)
	$(call make_artifact_full,linux,s390x)
	$(call make_artifact_full,linux,riscv64)

local: koko-ui
	$(call make_artifact_full,$(shell go env GOOS),$(shell go env GOARCH))

darwin-amd64: koko-ui
	$(call make_artifact_full,darwin,amd64)

darwin-arm64: koko-ui
	$(call make_artifact_full,darwin,arm64)

linux-amd64: koko-ui
	$(call make_artifact_full,linux,amd64)

linux-arm64: koko-ui
	$(call make_artifact_full,linux,arm64)

linux-loong64: koko-ui
	$(call make_artifact_full,linux,loong64)

linux-mips64le: koko-ui
	$(call make_artifact_full,linux,mips64le)

linux-ppc64le: koko-ui
	$(call make_artifact_full,linux,ppc64le)

linux-s390x: koko-ui
	$(call make_artifact_full,linux,s390x)

linux-riscv64: koko-ui
	$(call make_artifact_full,linux,riscv64)

koko-ui:
	@echo "build ui"
	@cd $(UIDIR) && $(NPMINSTALL) && $(NPMBUILD)

.PHONY: docker
docker:
	@echo "build docker images"
	docker buildx build --build-arg VERSION=$(VERSION) -t jumpserver/koko .

.PHONY: clean
clean:
	-rm -rf $(BUILDDIR)
	-rm -rf $(UIDIR)/dist/*

.PHONY: run
run:
	go run ./cmd/koko/

.PHONY: run-ui
run-ui:
	cd $(UIDIR) && yarn run serve