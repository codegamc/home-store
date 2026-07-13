#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
PACKAGE="$ROOT/synology/package"
ARCH=${1:-x86_64}
VERSION=${2:-dev}
BINARY=${3:-"$ROOT/bin/home-store-linux-${ARCH}"}
OUTPUT=${4:-"$ROOT/bin/home-store-${VERSION}-${ARCH}.spk"}

case "$BINARY" in /*) ;; *) BINARY="$ROOT/$BINARY" ;; esac
case "$OUTPUT" in /*) ;; *) OUTPUT="$ROOT/$OUTPUT" ;; esac
PACKAGE_VERSION=${VERSION#v}
case "$PACKAGE_VERSION" in
    *[!0-9._-]*|'') PACKAGE_VERSION="0.0.0-0001" ;;
esac

test -x "$BINARY"
mkdir -p "$ROOT/bin"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cp -R "$PACKAGE"/. "$TMP"/
mkdir -p "$TMP/package/bin"
cp "$BINARY" "$TMP/package/bin/home-store"
chmod 0755 "$TMP/package/bin/home-store" "$TMP/scripts/"*
sed "s/@VERSION@/$PACKAGE_VERSION/g; s/@ARCH@/$ARCH/g" "$PACKAGE/INFO" > "$TMP/INFO"
(cd "$TMP/package" && tar -czf "$TMP/package.tgz" .)
(cd "$TMP" && tar -cf "$OUTPUT" INFO package.tgz scripts conf LICENSE PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG)
printf '%s\n' "$OUTPUT"
