GO?=go
GOOS=$(shell ${GO} env GOOS)
GOARCH=$(shell ${GO} env GOARCH)
ACTIONLINT_VERSION=$(shell awk '/ACTIONLINT_VERSION:/ { print $$2 }' .github/workflows/main.yml)
EDITORCONFIG_CHECKER_VERSION=$(shell awk '/EDITORCONFIG_CHECKER_VERSION:/ { print $$2 }' .github/workflows/main.yml)
GOLANGCI_LINT_VERSION=$(shell awk '/GOLANGCI_LINT_VERSION:/ { print $$2 }' .github/workflows/main.yml)
GORELEASER_VERSION=$(shell awk '/GORELEASER_VERSION:/ { print $$2 }' .github/workflows/main.yml)
GOVERSIONINFO_VERSION=$(shell awk '/GOVERSIONINFO_VERSION:/ { print $$2 }' .github/workflows/main.yml)
UPSTREAM=$(shell git remote -v | awk '/github.com[:\/]twpayne\/chezmoi(.git)? \(fetch\)/ {print $$1}')
ifdef VERSION
	GO_LDFLAGS+=-X main.version=${VERSION}
endif
ifdef COMMIT
	GO_LDFLAGS+=-X main.commit=${COMMIT}
endif
ifdef DATE
	GO_LDFLAGS+=-X main.date=${DATE}
endif
ifdef BUILT_BY
	GO_LDFLAGS+=-X main.builtBy=${BUILT_BY}
endif
PREFIX?=/usr/local

.PHONY: default
default: build

.PHONY: smoke-test
smoke-test: run build-all test lint format

.PHONY: build
build:
ifeq (${GO_LDFLAGS},)
	${GO} build . || ( rm -f chezmoi ; false )
else
	${GO} build -ldflags "${GO_LDFLAGS}" . || ( rm -f chezmoi ; false )
endif

.PHONY: install
install: build
	mkdir -p "${DESTDIR}${PREFIX}/bin"
	install -m 755 --target-directory "${DESTDIR}${PREFIX}/bin" chezmoi

.PHONY: install-from-git-working-copy
install-from-git-working-copy:
	${GO} install -ldflags "-X main.version=$(shell git describe --abbrev=0 --tags) \
		-X main.commit=$(shell git rev-parse HEAD) \
		-X main.date=$(shell git show -s --format=%ct HEAD) \
		-X main.builtBy=source"

.PHONY: build-in-git-working-copy
build-in-git-working-copy:
	${GO} build -ldflags "-X main.version=$(shell git describe --abbrev=0 --tags) \
		-X main.commit=$(shell git rev-parse HEAD) \
		-X main.date=$(shell git show -s --format=%ct HEAD) \
		-X main.builtBy=source"

.PHONY: build-all
build-all: build-darwin build-freebsd build-linux build-windows

.PHONY: build-darwin
build-darwin:
	GOOS=darwin GOARCH=amd64 ${GO} build -o /dev/null .
	GOOS=darwin GOARCH=arm64 ${GO} build -o /dev/null .

.PHONY: build-freebsd
build-freebsd:
	GOOS=freebsd GOARCH=amd64 ${GO} build -o /dev/null .

.PHONY: build-linux
build-linux:
	GOOS=linux GOARCH=amd64 ${GO} build -o /dev/null .
	GOOS=linux GOARCH=amd64 ${GO} build -tags=noupgrade -o /dev/null .

.PHONY: build-windows
build-windows: create-syso
	GOOS=windows GOARCH=amd64 ${GO} build -o /dev/null .

.PHONY: run
run:
	${GO} tool chezmoi --version

.PHONY: test-all
test-all: test test-release rm-dist test-docker test-vagrant

.PHONY: rm-dist
rm-dist:
	rm -rf dist

.PHONY: test
test:
	${GO} test -ldflags="-X github.com/twpayne/chezmoi/internal/chezmoitest.umaskStr=0o022" ./...
	${GO} test -ldflags="-X github.com/twpayne/chezmoi/internal/chezmoitest.umaskStr=0o002" ./...

.PHONY: test-docker
test-docker:
	( cd assets/docker && ./test.sh alpine archlinux fedora )

.PHONY: test-vagrant
test-vagrant:
	( cd assets/vagrant && ./test.sh freebsd14 )

.PHONY: coverage-html
coverage-html: coverage
	${GO} tool cover -html=coverage.out

.PHONY: coverage
coverage:
	${GO} test -coverprofile=coverage.out -coverpkg=github.com/twpayne/chezmoi/... ./...

.PHONY: generate
generate:
	${GO} generate

.PHONY: lint
lint: ensure-actionlint ensure-editorconfig-checker ensure-golangci-lint shellcheck
	./bin/actionlint
	./bin/editorconfig-checker
	./bin/golangci-lint run
	${GO} tool lint-whitespace
	find . -name \*.txtar | xargs ${GO} run ./internal/cmds/lint-txtar
	${GO} tool find-typos chezmoi .
	${GO} tool lint-commit-messages ${UPSTREAM}/master..HEAD

.PHONY: lint-markdown
lint-markdown:
	markdownlint-cli2 --config .markdownlint-cli2.yaml

.PHONY: format
format: ensure-golangci-lint
	./bin/golangci-lint fmt
	find . -name \*.txtar | xargs ${GO} tool lint-txtar -w

.PHONY: format-yaml
format-yaml:
	find . -name \*.yaml -o -name \*.yml | xargs uv run task format-yaml

.PHONY: create-syso
create-syso: ensure-goversioninfo
	${GO} tool execute-template -output ./versioninfo.json ./assets/templates/versioninfo.json.tmpl
	./bin/goversioninfo -platform-specific

.PHONY: ensure-tools
ensure-tools: \
	ensure-actionlint \
	ensure-golangci-lint \
	ensure-goreleaser \
	ensure-goversioninfo

.PHONY: ensure-actionlint
ensure-actionlint:
	if [ ! -x bin/actionlint ] || ( ./bin/actionlint --version | grep -Fqv "v${ACTIONLINT_VERSION}" ) ; then \
		GOBIN=$(shell pwd)/bin ${GO} install "github.com/rhysd/actionlint/cmd/actionlint@v${ACTIONLINT_VERSION}" ; \
	fi

.PHONY: ensure-editorconfig-checker
ensure-editorconfig-checker:
	if [ ! -x bin/editorconfig-checker ] || ( ./bin/editorconfig-checker --version | grep -Fqv "v${EDITORCONFIG_CHECKER_VERSION}" ) ; then \
		curl -sSfL "https://github.com/editorconfig-checker/editorconfig-checker/releases/download/v${EDITORCONFIG_CHECKER_VERSION}/ec-${GOOS}-${GOARCH}.tar.gz" | tar -xzf - "bin/ec-${GOOS}-${GOARCH}" ; \
		mv "bin/ec-${GOOS}-${GOARCH}" bin/editorconfig-checker ; \
	fi

.PHONY: ensure-golangci-lint
ensure-golangci-lint:
	if [ ! -x bin/golangci-lint ] || ( ./bin/golangci-lint version | grep -Fqv "version ${GOLANGCI_LINT_VERSION}" ) ; then \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- v${GOLANGCI_LINT_VERSION} ; \
	fi

.PHONY: ensure-goreleaser
ensure-goreleaser:
	if [ ! -x bin/goreleaser ] || ( ./bin/goreleaser --version | grep -Fqv "${GORELEASER_VERSION}" ) ; then \
		GOBIN=$(shell pwd)/bin ${GO} install "github.com/goreleaser/goreleaser/v2@v${GORELEASER_VERSION}" ; \
	fi

.PHONY: ensure-goversioninfo
ensure-goversioninfo:
	if [ ! -x bin/goversioninfo ] ; then \
		GOBIN=$(shell pwd)/bin ${GO} install "github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v${GOVERSIONINFO_VERSION}" ; \
	fi

.PHONY: release
release: ensure-goreleaser
	./bin/goreleaser release \
		--clean \
		${GORELEASER_FLAGS}

.PHONY: shellcheck
shellcheck:
	find . -type f -name \*.sh | xargs shellcheck

.PHONY: test-release
test-release: ensure-goreleaser
	./bin/goreleaser release \
		--clean \
		--skip=chocolatey,sign \
		--snapshot \
		${GORELEASER_FLAGS}
