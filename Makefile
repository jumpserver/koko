NAME=koko
BUILDDIR=build

VERSION ?= Unknown
BuildTime := $(shell date -u '+%Y-%m-%d %I:%M:%S%p')
COMMIT := $(shell git rev-parse HEAD)
GOVERSION := $(shell go version)
CipherKey := $(shell head -c 100 /dev/urandom | base64 | head -c 32)

BASEPATH := $(shell pwd)
KOKOSRCFILE := $(BASEPATH)/cmd/koko/

GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

LDFLAGS=-w -s

KOKOLDFLAGS+=-X 'main.Buildstamp=$(BuildTime)'
KOKOLDFLAGS+=-X 'main.Githash=$(COMMIT)'
KOKOLDFLAGS+=-X 'main.Goversion=$(GOVERSION)'
KOKOLDFLAGS+=-X 'main.Version=$(VERSION)'
KOKOLDFLAGS+=-X 'github.com/jumpserver/koko/pkg/config.CipherKey=$(CipherKey)'

KOKOBUILD=CGO_ENABLED=1 go build -trimpath -ldflags "$(KOKOLDFLAGS) ${LDFLAGS}"

UIDIR=ui

define make_artifact_full
	GOOS=$(1) GOARCH=$(2) $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$(1)-$(2) $(KOKOSRCFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/locale/
	cp $(BUILDDIR)/$(NAME)-$(1)-$(2) $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/$(NAME)
	cp README.md $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/README.md
	cp LICENSE $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/LICENSE
	cp config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/config_example.yml
	cp entrypoint.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/entrypoint.sh
	cp utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/init-kubectl.sh
	cp -r locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/locale/

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$(1)-$(2).tar.gz $(NAME)-$(VERSION)-$(1)-$(2)
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2) $(BUILDDIR)/$(NAME)-$(1)-$(2)
endef

.PHONY: build
build:
	mkdir -p $(BUILDDIR)
	GOARCH=$(GOARCH) GOOS=$(GOOS) $(KOKOBUILD) -o $(BUILDDIR)/$(NAME) $(KOKOSRCFILE)

all:
	$(call make_artifact_full,darwin,amd64)
	$(call make_artifact_full,darwin,arm64)
	$(call make_artifact_full,linux,amd64)
	$(call make_artifact_full,linux,arm64)
	$(call make_artifact_full,linux,mips64le)
	$(call make_artifact_full,linux,ppc64le)
	$(call make_artifact_full,linux,s390x)
	$(call make_artifact_full,linux,riscv64)
	$(call make_artifact_full,linux,loong64)

local:
	$(call make_artifact_full,$(shell go env GOOS),$(shell go env GOARCH))

darwin-amd64:
	$(call make_artifact_full,darwin,amd64)

darwin-arm64:
	$(call make_artifact_full,darwin,arm64)

linux-amd64:
	$(call make_artifact_full,linux,amd64)

linux-arm64:
	$(call make_artifact_full,linux,arm64)

linux-loong64:
	$(call make_artifact_full,linux,loong64)

linux-mips64le:
	$(call make_artifact_full,linux,mips64le)

linux-ppc64le:
	$(call make_artifact_full,linux,ppc64le)

linux-s390x:
	$(call make_artifact_full,linux,s390x)

linux-riscv64:
	$(call make_artifact_full,linux,riscv64)

koko-ui:
	@echo "build ui"
	@cd $(UIDIR) && yarn install && yarn build

.PHONY: docker
docker:
	@echo "build docker images"
	docker buildx build --build-arg VERSION=$(VERSION) -t jumpserver/koko:$(VERSION)-ce . --load

.PHONY: docker-ee
docker-ee:docker
	@echo "build docker images"
	docker buildx build --build-arg VERSION=$(VERSION) -t jumpserver/koko-ee:$(VERSION)-ce -f Dockerfile-ee . --load

.PHONY: clean
clean:
	-rm -rf $(BUILDDIR)
	-rm -rf $(UIDIR)/dist/*

.PHONY: libghostty-vt
libghostty-vt:
	@./utils/setup-libghostty-vt.sh

.PHONY: guacd
guacd:
	docker compose -f docker-compose-guacd.yml up -d

.PHONY: run
run: guacd
	@cleanup() { \
		status=$$?; \
		trap - EXIT INT TERM; \
		docker compose -f docker-compose-guacd.yml down; \
		exit $$status; \
	}; \
	trap cleanup EXIT; \
	trap 'exit 130' INT; \
	trap 'exit 143' TERM; \
	LIBGHOSTTY_VT_ROOT="$$(./utils/setup-libghostty-vt.sh)"; \
	PKG_CONFIG_PATH="$${LIBGHOSTTY_VT_ROOT}/lib/pkgconfig$${PKG_CONFIG_PATH:+:$${PKG_CONFIG_PATH}}" \
	CGO_ENABLED=1 go run ./cmd/koko/

.PHONY: run-ui
run-ui:
	cd $(UIDIR) && yarn run serve
