.PHONY: build test install lint catalog clean dist

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
PLATFORMS := linux/amd64 linux/arm64
MAN5 ?= /usr/share/man/man5

build:
	go build -o bin/sysdtui ./cmd/sysdtui

test:
	go test ./...

install:
	go install ./cmd/sysdtui

lint:
	gofmt -l . | (! grep .)
	go vet ./...

# Regenerate the embedded directive catalog from systemd's man pages.
catalog:
	go run ./cmd/gencatalog -man $(MAN5) -version "$$(systemctl --version | awk 'NR==1{print $$2}')"

dist:
	rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; name=sysdtui_$(VERSION)_$${os}_$${arch}; \
		echo "building $$name"; \
		mkdir -p dist/$$name && \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath \
			-ldflags "-s -w -X main.version=$(VERSION)" -o dist/$$name/sysdtui ./cmd/sysdtui && \
		cp LICENSE NOTICE README.md docs/sysdtui.1 dist/$$name/ && \
		tar -C dist -czf dist/$$name.tar.gz $$name && rm -rf dist/$$name || exit 1; \
	done
	cd dist && sha256sum *.tar.gz > checksums.txt

clean:
	rm -rf bin dist
