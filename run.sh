#!/bin/sh

echo "Starting..."
go run -ldflags "-X main.curVersion=$(git describe --always --long) -X 'main.curBuild=$(date)'" main.go
