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
KOKOLDFLAGS+=-X 'github.com/jumpserver/koko/pkg/koko.Version=$(VERSION)'
KOKOLDFLAGS+=-X 'github.com/jumpserver/koko/pkg/config.CipherKey=$(CipherKey)'

K8SCMDFLAGS=-X 'github.com/jumpserver/koko/pkg/config.CipherKey=$(CipherKey)'

KOKOBUILD=CGO_ENABLED=0 go build -trimpath -ldflags "$(KOKOLDFLAGS) ${LDFLAGS}"
K8SCMDBUILD=CGO_ENABLED=0 go build -trimpath -ldflags "$(K8SCMDFLAGS) ${LDFLAGS}"

PLATFORM_LIST = \
	darwin-amd64 \
	darwin-arm64 \
	linux-amd64 \
	linux-arm64

all-arch: $(PLATFORM_LIST)

darwin-amd64:koko-ui
	GOARCH=amd64 GOOS=darwin $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$@ $(KOKOSRCFILE)
	GOARCH=amd64 GOOS=darwin $(K8SCMDBUILD) -o $(BUILDDIR)/kubectl-$@ $(KUBECTLFILE)
	GOARCH=amd64 GOOS=darwin $(K8SCMDBUILD) -o $(BUILDDIR)/helm-$@ $(HELMFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/

	cp $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/$(NAME)
	cp $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/kubectl
	cp $(BUILDDIR)/helm-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/helm
	cp -r $(BASEPATH)/locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	cp -r $(BASEPATH)/config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$@/config_example.yml
	cp -r $(BASEPATH)/utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$@/init-kubectl.sh

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$@.tar.gz $(NAME)-$(VERSION)-$@
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$@ $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/helm-$@

darwin-arm64:koko-ui
	GOARCH=arm64 GOOS=darwin $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$@ $(KOKOSRCFILE)
	GOARCH=arm64 GOOS=darwin $(K8SCMDBUILD) -o $(BUILDDIR)/kubectl-$@ $(KUBECTLFILE)
	GOARCH=arm64 GOOS=darwin $(K8SCMDBUILD) -o $(BUILDDIR)/helm-$@ $(HELMFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/

	cp $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/$(NAME)
	cp $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/kubectl
	cp $(BUILDDIR)/helm-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/helm
	cp -r $(BASEPATH)/locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	cp -r $(BASEPATH)/config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$@/config_example.yml
	cp -r $(BASEPATH)/utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$@/init-kubectl.sh

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$@.tar.gz $(NAME)-$(VERSION)-$@
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$@ $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/helm-$@

linux-amd64:koko-ui
	GOARCH=amd64 GOOS=linux $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$@ $(KOKOSRCFILE)
	GOARCH=amd64 GOOS=linux $(K8SCMDBUILD) -o $(BUILDDIR)/kubectl-$@ $(KUBECTLFILE)
	GOARCH=amd64 GOOS=linux $(K8SCMDBUILD) -o $(BUILDDIR)/helm-$@ $(HELMFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/

	cp $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/$(NAME)
	cp $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/kubectl
	cp $(BUILDDIR)/helm-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/helm
	cp -r $(BASEPATH)/locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	cp -r $(BASEPATH)/config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$@/config_example.yml
	cp -r $(BASEPATH)/utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$@/init-kubectl.sh

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$@.tar.gz $(NAME)-$(VERSION)-$@
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$@ $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/helm-$@

linux-arm64:koko-ui
	GOARCH=arm64 GOOS=linux $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$@ $(KOKOSRCFILE)
	GOARCH=arm64 GOOS=linux $(K8SCMDBUILD) -o $(BUILDDIR)/kubectl-$@ $(KUBECTLFILE)
	GOARCH=arm64 GOOS=linux $(K8SCMDBUILD) -o $(BUILDDIR)/helm-$@ $(HELMFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/

	cp $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/$(NAME)
	cp $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/kubectl
	cp $(BUILDDIR)/helm-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/helm
	cp -r $(BASEPATH)/locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	cp -r $(BASEPATH)/config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$@/config_example.yml
	cp -r $(BASEPATH)/utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$@/init-kubectl.sh

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$@.tar.gz $(NAME)-$(VERSION)-$@
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$@ $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/kubectl-$@

linux-loong64:koko-ui
	GOARCH=loong64 GOOS=linux $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$@ $(KOKOSRCFILE)
	GOARCH=loong64 GOOS=linux $(K8SCMDBUILD) -o $(BUILDDIR)/kubectl-$@ $(KUBECTLFILE)
	GOARCH=loong64 GOOS=linux $(K8SCMDBUILD) -o $(BUILDDIR)/helm-$@ $(HELMFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/

	cp $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/$(NAME)
	cp $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/kubectl
	cp $(BUILDDIR)/helm-$@ $(BUILDDIR)/$(NAME)-$(VERSION)-$@/helm
	cp -r $(BASEPATH)/locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$@/locale/
	cp -r $(BASEPATH)/config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$@/config_example.yml
	cp -r $(BASEPATH)/utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$@/init-kubectl.sh

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$@.tar.gz $(NAME)-$(VERSION)-$@
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$@ $(BUILDDIR)/$(NAME)-$@ $(BUILDDIR)/kubectl-$@ $(BUILDDIR)/helm-$@

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
