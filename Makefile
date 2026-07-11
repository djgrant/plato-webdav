GOFLAGS := -trimpath -ldflags "-s -w"

.PHONY: build kobo test clean

build:
	go build $(GOFLAGS) -o plato-webdav .

# Kobo devices are ARMv7 Linux; CGO off gives a fully static binary.
kobo:
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build $(GOFLAGS) -o dist/plato-webdav .

test:
	go vet ./...
	go test ./...

clean:
	rm -rf plato-webdav dist
