#!/bin/sh

mkdir -p "Build"

# 【darwin/amd64】
echo "start build darwin/amd64 >>>"
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags '-w -s' -o ./Build/Nas-Everything-search-macOS-amd64 main.go

# 【windows/amd64】
echo "start build windows/amd64 >>>"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags '-w -s' -o ./Build/Nas-Everything-search-Windows-amd64.exe main.go

# 【linux/amd64】
echo "start build linux/amd64 >>>"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-w -s' -o ./Build/Nas-Everything-search-Linux-amd64 main.go

# 【linux/arm64】
echo "start build linux/arm64 >>>"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags '-w -s' -o ./Build/Nas-Everything-search-Linux-arm64 main.go

echo "All build success!!!"
