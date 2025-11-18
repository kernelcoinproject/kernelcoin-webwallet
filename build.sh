#!/bin/bash
set -e

mkdir -p release

package() {
    GOOS=$1
    GOARCH=$2
    EXT=$3
    NAME=$4

    BIN="wallet-server-$NAME$EXT"

    echo "Building $BIN..."

    GOOS=$GOOS GOARCH=$GOARCH go build -o "$BIN" main.go rpc_client.go wallet.go

    tar -czf "release/$BIN.tar.gz" "$BIN" index.html
}

package linux   amd64 ""     lin-x86_x64
package windows amd64 ".exe" win-x86_64
package darwin  amd64 ""     osx-x86_64
package darwin  arm64 ""     osx-arm

echo "All builds completed."
echo "Tarballs created under ./release:"
ls -1 release

