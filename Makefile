# This Source Code Form is subject to the terms of the Mozilla Public
# License, version 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at http://mozilla.org/MPL/2.0/.

GHACCOUNT := johansundell
# The GitHub repository releases are published to; NAME is the server
# binary and Docker image.
GHREPO := scribble
NAME := scribble-server
VERSION := v1.2.0

build:
	go build -ldflags "-X 'main.Version=$(VERSION)'"

# Backs up the database with a freshly built binary (scribble-server backup). It reads
# SQLITE_PATH, BACKUP_DIR and RETENTION_DAYS from .env; pass flags with ARGS,
# e.g. make backup ARGS="-dir /srv/backups" or ARGS=-cron-snippet.
backup: build
	./$(NAME) backup $(ARGS)


# The command-line client (build/ holds both for `make compile`).
build-cli:
	go build -ldflags "-X 'main.Version=$(VERSION)'" -o sn ./cmd/sn

# The Fyne desktop client. Needs CGO and a C compiler; on Linux also the
# GL, X11, Wayland and xkbcommon development headers (see README).
build-gui:
	go build -ldflags "-X 'main.Version=$(VERSION)'" -o scribble-gui ./cmd/scribble-gui

# compile builds the server and sn for every platform, and the desktop app
# for Windows only. The desktop app is released for Windows and macOS (#133):
# the "Release macOS GUI" workflow builds the macOS one on a Mac and uploads
# it to the release. On Linux, build it from source with make build-gui.
#
# Fyne's Windows packager embeds only the largest image of an .ico, so
# cmd/winicon then swaps the exe's icons for every size of icon.ico (#123).
# fyne-cross gets Icon.png, so fyne.AppMetadata holds the 512px image rather
# than the 256px one scaled up.
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
	-output "build/scribble_$(VERSION)_{{.OS}}_{{.Arch}}/sn" \
	./cmd/sn
	@fyne-cross windows \
	-arch=amd64 \
	-env=GOTOOLCHAIN=auto \
	-app-id=com.johansundell.scribble \
	-app-version=$(patsubst v%,%,$(VERSION)) \
	-icon=cmd/scribble-gui/Icon.png \
	-name=scribble-gui.exe \
	-ldflags="-X=main.Version=$(VERSION)" \
	./cmd/scribble-gui
	@go run ./cmd/winicon -ico cmd/scribble-gui/icon.ico fyne-cross/bin/windows-amd64/scribble-gui.exe
	@cp fyne-cross/bin/windows-amd64/scribble-gui.exe build/scribble_$(VERSION)_windows_amd64/scribble-gui.exe
	@rm -rf fyne-cross/

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

release: dist #docker-push
	@latest_tag=$$(git describe --tags `git rev-list --tags --max-count=1`); \
	comparison="$$latest_tag..HEAD"; \
	if [ -z "$$latest_tag" ]; then comparison=""; fi; \
	changelog=$$(git log $$comparison --oneline --no-merges); \
	github-release $(GHACCOUNT)/$(GHREPO) $(VERSION) "$$(git rev-parse --abbrev-ref HEAD)" "**Changelog**<br/>$$changelog" 'dist/*'; \
	git pull

# Renders every app icon from the vector source (#106). The maskable and
# Apple icons are the square without its rounded corners, since launchers and
# iOS cut their own shape; the artwork already sits inside the maskable safe
# zone (the middle 80%). Each .ico size is rendered on its own, so the small
# ones stay sharp.
ICON_SRC := assets/img/icon.svg
icons:
	rsvg-convert -w 512 -h 512 $(ICON_SRC) -o assets/img/icon-512.png
	rsvg-convert -w 192 -h 192 $(ICON_SRC) -o assets/img/icon-192.png
	rsvg-convert -w 64 -h 64 $(ICON_SRC) -o assets/img/favicon.png
	sed 's/ rx="115"//' $(ICON_SRC) | rsvg-convert -w 512 -h 512 | magick png:- -alpha off -strip assets/img/icon-maskable-512.png
	sed 's/ rx="115"//' $(ICON_SRC) | rsvg-convert -w 180 -h 180 | magick png:- -alpha off -strip assets/img/apple-touch-icon.png
	d=$$(mktemp -d) && for s in 16 24 32 48 64 128 256; do rsvg-convert -w $$s -h $$s $(ICON_SRC) -o $$d/$$s.png; done && magick $$d/*.png assets/img/icon.ico && rm -r $$d
	cp assets/img/icon-512.png cmd/scribble-gui/Icon.png
	cp assets/img/icon.ico cmd/scribble-gui/icon.ico

# Bumps version, runs tests, commits and pushes to master, and runs make release.
# Usage: make bump-release [BUMP=patch|minor|major|vX.Y.Z] [DRY_RUN=1]
bump-release:
	@DRY_RUN=$(DRY_RUN) ./scripts/bump-release.sh $(BUMP)

.PHONY: build backup build-cli build-gui compile install deps dist release docker docker-push docker-run bump-release icons
