#!/bin/sh
# Build DockIt's .deb packages, for amd64 and arm64, into dist/:
#
#   packaging/build-deb.sh [version]
#
# The version defaults to `git describe --tags --always --dirty`.  A release,
# vX.Y.Z, must match the dataset format (see internal/buildcheck) and makes
# package version X.Y.Z.  Anything else, such as v2.2.6-3-gabc1234-dirty,
# makes 2.2.6+3.gabc1234.dirty, which apt puts after 2.2.6 and before 2.2.7.
# Needs Go, and nothing else: nFPM is run with `go run`.
set -eu

NFPM=github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.47.0

cd "$(dirname "$0")/.."
VERSION=${1:-$(git describe --tags --always --dirty)}
go run ./internal/buildcheck "$VERSION"

PKG_VERSION=$(echo "${VERSION#v}" | sed 's/-/+/; s/-/./g')
case $PKG_VERSION in
[0-9]*) ;;
*)
	echo "build-deb: $VERSION does not start with a version number" >&2
	exit 1
	;;
esac

# Not GOARCH for nFPM: `go run` would build nFPM for that architecture.
for arch in amd64 arm64; do
	CGO_ENABLED=0 GOOS=linux GOARCH=$arch \
		go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o dist/$arch/dockit ./cmd/dockit
	DOCKIT_ARCH=$arch DOCKIT_PKG_VERSION=$PKG_VERSION \
		go run $NFPM package -f packaging/nfpm.yaml -p deb -t dist/
done
