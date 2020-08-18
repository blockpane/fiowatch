#!/bin/bash

# builds a macos package (.app) and places it inside a compressed .dmg

mkdir -p "package/FIO Watch"
mkdir -p package/old
mv package/*.dmg package/old/

#GOOS=darwin go build -ldflags "-s -w" -o cmd/contract-explorer/contract-explorer cmd/contract-explorer/main.go
go build -ldflags "-s -w" -o cmd/fio-watch/fio-watch cmd/fio-watch/main.go

upx -9 cmd/fio-watch/fio-watch
fyne package -sourceDir cmd/contract-explorer -name "FIO Watch" -os darwin && mv "FIO Watch.app" "package/FIO watch/"
sed -i'' -e 's/.string.1\.0.\/string./\<string>'$(git describe --tags --always --long)'\<\/string>/g' "package/FIO watch/FIO watch.app/Contents/Info.plist"
rm -f cmd/fio-watch/fio-watch
pushd package
ls -alh
# hdiutil create -srcfolder "FIO contract-explorer" -format UDBZ "FIO contract-explorer.dmg"
hdiutil create -srcfolder "FIO contract-explorer" "FIO contract-explorer.dmg"
popd
rm -fr "package/FIO contract-explorer"
open "package/FIO contract-explorer.dmg"

