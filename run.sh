#!/bin/sh

echo "Starting..."
podman run --rm \
-v $PWD:/app docker.io/dirkw85/dev-golang:latest \
go run -ldflags "-X main.curVersion=$(git rev-list --count HEAD)-$(git describe --always --long) -X 'main.curBuild=$(date)'" main.go
