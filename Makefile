.PHONY: build pack
build:
	mkdir -p bin
	go build -trimpath -o bin/autodownload-worker ./cmd/autodownload-worker

pack:
	bash scripts/pack.sh dist
