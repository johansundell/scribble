# This Source Code Form is subject to the terms of the Mozilla Public
# License, version 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at http://mozilla.org/MPL/2.0/.

GHACCOUNT := johansundell
NAME := scribble-server
VERSION := v0.0.8

build:
	go build -ldflags "-X 'main.Version=$(VERSION)'"

# The command-line client (build/ holds both for `make compile`).
build-cli:
	go build -ldflags "-X 'main.Version=$(VERSION)'" -o scribble ./cmd/scribble

# The Fyne desktop client. Needs CGO and a C compiler; on Linux also the
# GL, X11, Wayland and xkbcommon development headers (see README).
build-gui:
	go build -ldflags "-X 'main.Version=$(VERSION)'" -o scribble-gui ./cmd/scribble-gui

compile:
	@rm -rf build/ fyne-cross/
	@gox -ldflags "-X 'main.Version=$(VERSION)'" \
	-osarch="darwin/amd64" \
	-osarch="linux/amd64" \
	-osarch="linux/arm64" \
	-osarch="windows/amd64" \
	-output "build/{{.Dir}}_$(VERSION)_{{.OS}}_{{.Arch}}/$(NAME)" \
	.
	@gox -ldflags "-X 'main.Version=$(VERSION)'" \
	-osarch="darwin/amd64" \
	-osarch="darwin/arm64" \
	-osarch="linux/amd64" \
	-osarch="linux/arm64" \
	-osarch="windows/amd64" \
	-output "build/scribble_$(VERSION)_{{.OS}}_{{.Arch}}/scribble" \
	./cmd/scribble
	@fyne-cross linux \
	-arch=amd64 \
	-env=GOTOOLCHAIN=auto \
	-app-id=com.johansundell.scribble \
	-app-version=$(patsubst v%,%,$(VERSION)) \
	-icon=assets/img/logo.png \
	-name=scribble-gui \
	-ldflags="-X=main.Version=$(VERSION)" \
	./cmd/scribble-gui
	@cp fyne-cross/bin/linux-amd64/scribble-gui build/scribble_$(VERSION)_linux_amd64/scribble-gui
	@fyne-cross windows \
	-arch=amd64 \
	-env=GOTOOLCHAIN=auto \
	-app-id=com.johansundell.scribble \
	-app-version=$(patsubst v%,%,$(VERSION)) \
	-icon=assets/img/logo.png \
	-name=scribble-gui.exe \
	-ldflags="-X=main.Version=$(VERSION)" \
	./cmd/scribble-gui
	@cp fyne-cross/bin/windows-amd64/scribble-gui.exe build/scribble_$(VERSION)_windows_amd64/scribble-gui.exe
	@rm -rf fyne-cross/

##	@gox -ldflags "-X 'main.Version=$(VERSION)'" \
#	-osarch="darwin/amd64" \
#	-osarch="darwin/arm64" \
#	-osarch="linux/amd64" \
#	-osarch="linux/arm64" \
#	-osarch="windows/amd64" \
#	-output "build/scribble-gui_$(VERSION)_{{.OS}}_{{.Arch}}/scribble-gui" \
#	./cmd/scribble-gui

install:
	go install -ldflags "-X main.Version=$(VERSION)"

# Installs the release tools into $(go env GOPATH)/bin (or GOBIN), which must
# be on PATH for compile, dist and release. go get no longer installs binaries.
deps:
	go install github.com/c4milo/github-release@latest
	go install github.com/mitchellh/gox@latest
	go install github.com/fyne-io/fyne-cross@latest

dist: compile
	$(eval FILES := $(shell ls build))
	@rm -rf dist && mkdir dist
	@for f in $(FILES); do \
		(cd $(shell pwd)/build/$$f && tar -cvzf ../../dist/$$f.tar.gz *); \
		(cd $(shell pwd)/dist && shasum -a 512 $$f.tar.gz > $$f.sha512); \
		echo $$f; \
	done

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(GHACCOUNT)/$(NAME):$(VERSION) .

docker-push: docker
	docker push $(GHACCOUNT)/$(NAME):$(VERSION)

docker-run:
	VERSION=$(VERSION) docker compose up -d --build

release: dist docker-push
	@latest_tag=$$(git describe --tags `git rev-list --tags --max-count=1`); \
	comparison="$$latest_tag..HEAD"; \
	if [ -z "$$latest_tag" ]; then comparison=""; fi; \
	changelog=$$(git log $$comparison --oneline --no-merges); \
	github-release $(GHACCOUNT)/$(NAME) $(VERSION) "$$(git rev-parse --abbrev-ref HEAD)" "**Changelog**<br/>$$changelog" 'dist/*'; \
	git pull

.PHONY: build build-cli build-gui compile install deps dist release docker docker-push docker-run
