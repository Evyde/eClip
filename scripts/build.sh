#!/bin/bash

GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o eclip.exe ./cmd/eclip
GOOS=darwin GOARCH=arm64 go build -o eclip_mac ./cmd/eclip