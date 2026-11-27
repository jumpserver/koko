NAME=koko
BUILDDIR=build
GOLANGCI_LINT ?= golangci-lint

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

# usql remains a separate module and PTY child. Do not apply KoKo's driver
# replacements or CGO settings to its build.
define build_usql
	GOWORK=off CGO_ENABLED=0 GOOS=$(1) GOARCH=$(2) go -C tools/usql build -mod=readonly -trimpath -ldflags "$(LDFLAGS) -X 'github.com/xo/usql/text.CommandVersion=$(VERSION)'" -o "$(abspath $(3))" .
endef

define make_artifact_full
	GOOS=$(1) GOARCH=$(2) $(KOKOBUILD) -o $(BUILDDIR)/$(NAME)-$(1)-$(2) $(KOKOSRCFILE)
	mkdir -p $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/locale/
	$(call build_usql,$(1),$(2),$(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/usql)
	cp $(BUILDDIR)/$(NAME)-$(1)-$(2) $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/$(NAME)
	cp README.md $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/README.md
	cp LICENSE $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/LICENSE
	cp config_example.yml $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/config_example.yml
	cp entrypoint.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/entrypoint.sh
	cp utils/init-kubectl.sh $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/init-kubectl.sh
	cp -r locale/* $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)/locale/
	$(MAKE) bundle-resources DESTDIR="$(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2)"

	cd $(BUILDDIR) && tar -czvf $(NAME)-$(VERSION)-$(1)-$(2).tar.gz $(NAME)-$(VERSION)-$(1)-$(2)
	rm -rf $(BUILDDIR)/$(NAME)-$(VERSION)-$(1)-$(2) $(BUILDDIR)/$(NAME)-$(1)-$(2)
endef

define build_koko
	@set -e; \
	if ! "$${PKG_CONFIG:-pkg-config}" --exists libghostty-vt-static; then \
		libghostty_dir="$$(./utils/setup-libghostty-vt.sh)"; \
		export PKG_CONFIG_PATH="$$libghostty_dir/lib/pkgconfig$${PKG_CONFIG_PATH:+:$$PKG_CONFIG_PATH}"; \
	fi; \
	GOARCH=$(GOARCH) GOOS=$(GOOS) $(KOKOBUILD) -o "$(1)" $(KOKOSRCFILE)
endef

.PHONY: build
build: usql
	$(call build_koko,$(BUILDDIR)/$(NAME))
	GOARCH=$(GOARCH) GOOS=$(GOOS) CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS) -X 'main.Version=$(VERSION)'" -o "$(BUILDDIR)/kael" ./cmd/kael

.PHONY: chen-resources
chen-resources:
	test -n "$(DESTDIR)"
	mkdir -p "$(DESTDIR)/licenses/chen"
	cp pkg/chen/standalone/standalone_backends_example.yaml "$(DESTDIR)/"
	for driver in omni go-db2 go-ora; do cp "pkg/chen/third_party/$$driver/LICENSE" "$(DESTDIR)/licenses/chen/$$driver-LICENSE" || exit 1; done

.PHONY: bundle-resources
bundle-resources: chen-resources
	cp tools/usql/UPSTREAM-LICENSE "$(DESTDIR)/licenses/usql-LICENSE"

all:
	$(call make_artifact_full,darwin,arm64)
	$(call make_artifact_full,linux,amd64)
	$(call make_artifact_full,linux,arm64)

local:
	$(call make_artifact_full,$(shell go env GOOS),$(shell go env GOARCH))

darwin-arm64:
	$(call make_artifact_full,darwin,arm64)

linux-amd64:
	$(call make_artifact_full,linux,amd64)

linux-arm64:
	$(call make_artifact_full,linux,arm64)

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

.PHONY: libghostty-vt
libghostty-vt:
	@./utils/setup-libghostty-vt.sh

.PHONY: lint
lint:
	@$(GOLANGCI_LINT) version
	@$(GOLANGCI_LINT) config verify
	@LIBGHOSTTY_VT_ROOT="$$(./utils/setup-libghostty-vt.sh)" || exit $$?; \
	PKG_CONFIG_PATH="$${LIBGHOSTTY_VT_ROOT}/lib/pkgconfig$${PKG_CONFIG_PATH:+:$${PKG_CONFIG_PATH}}" \
	CGO_ENABLED=1 $(GOLANGCI_LINT) run

.PHONY: usql
usql:
	$(call build_usql,$(GOOS),$(GOARCH),$(BUILDDIR)/usql)

.PHONY: guacd
guacd:
	docker compose -f docker-compose-guacd.yml up -d

.PHONY: run dev
run dev: KOKOBUILD = CGO_ENABLED=1 go build -trimpath
run dev:
	$(call build_koko,$(BUILDDIR)/dev/koko)
	$(call build_usql,$(GOOS),$(GOARCH),$(BUILDDIR)/dev/usql)
	@./utils/run-dev.sh "$@" "$(abspath $(BUILDDIR)/dev)"
