#!/bin/bash

VER=$(git describe --tags --always --long)

# this is screwed up, somehow go mod keeps wanting to pull this back to an
# earlier version. pfft. Force it to update to the correct version
go get github.com/wcharczuk/go-chart@2d5aeaf824f4b3be7c0b27ea89327eeff6b6e93a

# first some standard packages:
fyne-cross windows -arch 386 -output fiowatch-${VER}-386.exe cmd/fio-watch/main.go
fyne-cross windows -output fiowatch-${VER}-amd64.exe cmd/fio-watch/main.go
fyne-cross linux -output fiowatch-${VER}-linux-amd64 cmd/fio-watch/main.go
fyne-cross linux -arch 386 -output fiowatch-${VER}-linux-x86 cmd/fio-watch/main.go

export CGO_CFLAGS="-mmacosx-version-min=10.14"
export CGO_LDFLAGS="-mmacosx-version-min=10.14"
# builds a macos package (.app) and places it inside a .dmg

mkdir -p "package/FIO Watch"
mkdir -p package/old
mv package/*.dmg package/old/

go build -ldflags "-s -w" -o cmd/fio-watch/fio-watch cmd/fio-watch/main.go

# I've had mixed results with compressed .dmg images, some people have complained, compress the binary instead:
upx -9 cmd/fio-watch/fio-watch

fyne package -sourceDir cmd/fio-watch -name "FIO Watch" -os darwin && mv "FIO Watch.app" "package/FIO Watch/"
sed -i'' -e 's/.string.1\.0.\/string./\<string>'$(git describe --tags --always --long)'\<\/string>/g' "package/FIO Watch/FIO Watch.app/Contents/Info.plist"

rm -f cmd/fio-watch/fio-watch
pushd package
hdiutil create -srcfolder "FIO Watch" "FIO Watch ${VER}.dmg"
popd

rm -fr "package/FIO Watch"
open "package/FIO Watch ${VER}.dmg"
open fyne-cross/dist

