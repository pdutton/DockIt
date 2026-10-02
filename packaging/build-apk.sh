#!/bin/sh
# Build DockIt's APK packages, for Alpine Linux on x86_64 and aarch64, into
# dist/:
#
#   packaging/build-apk.sh [version]
#
# The version defaults to `git describe --tags --always --dirty`.  A release,
# vX.Y.Z, must match the dataset format (see internal/buildcheck) and makes
# package version X.Y.Z-r0.  An Alpine version cannot hold a commit hash, so
# a build N commits after a tag makes X.Y.Z_gitN-r0, and a changed tree adds
# the time, as in 2.2.7_git3_p20261001190000-r0.  apk puts both after X.Y.Z
# and before the next release.  Needs Go, and nothing else: nFPM is run with
# `go run`.
set -eu

NFPM=github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.47.0

cd "$(dirname "$0")/.."
VERSION=${1:-$(git describe --tags --always --dirty)}
go run ./internal/buildcheck "$VERSION"

v=${VERSION#v}
dirty=
case $v in
*-dirty)
	v=${v%-dirty}
	dirty=_p$(date -u +%Y%m%d%H%M%S)
	;;
esac
case $v in
*-*-g*)
	# X.Y.Z-N-gHASH
	n=${v#*-}
	PKG_VERSION=${v%%-*}_git${n%%-*}$dirty
	;;
*)
	PKG_VERSION=$v${dirty:+_git0$dirty}
	;;
esac
if ! echo "$PKG_VERSION" | grep -Eq '^[0-9]+(\.[0-9]+)*(_git[0-9]+)?(_p[0-9]+)?$'; then
	echo "build-apk: cannot make an Alpine package version from $VERSION" >&2
	exit 1
fi

# Not GOARCH for nFPM: `go run` would build nFPM for that architecture.
for arch in amd64 arm64; do
	CGO_ENABLED=0 GOOS=linux GOARCH=$arch \
		go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o dist/$arch/dockit ./cmd/dockit
	DOCKIT_ARCH=$arch DOCKIT_PKG_VERSION=$PKG_VERSION \
		go run $NFPM package -f packaging/nfpm-apk.yaml -p apk -t dist/
done
