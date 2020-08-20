#!/bin/bash

export CGO_CFLAGS="-mmacosx-version-min=10.14"
export CGO_LDFLAGS="-mmacosx-version-min=10.14"
# builds a macos package (.app) and places it inside a .dmg

mkdir -p "package/FIO Watch"
mkdir -p package/old
mv package/*.dmg package/old/

go build -ldflags "-s -w" -o cmd/fio-watch/fio-watch cmd/fio-watch/main.go

# I've had mixed results with compressed .dmg images, some people have complained, compress the binary instead:
upx -9 cmd/fio-watch/fio-watch

fyne package -sourceDir cmd/fio-watch -name "FIO Watch" -os darwin && mv "FIO Watch.app" "package/FIO watch/"
sed -i'' -e 's/.string.1\.0.\/string./\<string>'$(git describe --tags --always --long)'\<\/string>/g' "package/FIO watch/FIO watch.app/Contents/Info.plist"

rm -f cmd/fio-watch/fio-watch
pushd package
hdiutil create -srcfolder "FIO watch" "FIO watch.dmg"
popd

rm -fr "package/FIO fio-watch"
open "package/FIO watch.dmg"

