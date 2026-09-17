#!/bin/sh
# Builds the binaries, stages the npm packages and installs them from packed
# tarballs, so the whole npm path is exercised without a release or a registry.
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=0.0.0-test

case $(uname -s) in
Darwin) host_os=darwin ;;
Linux) host_os=linux ;;
*)
	echo "npm/test.sh: unsupported host $(uname -s)" >&2
	exit 1
	;;
esac
case $(uname -m) in
x86_64 | amd64) host_arch=x64 ;;
arm64 | aarch64) host_arch=arm64 ;;
*)
	echo "npm/test.sh: unsupported host $(uname -m)" >&2
	exit 1
	;;
esac

goreleaser build --snapshot --clean

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# stage.sh writes the version and the binaries into the tree it lives in, which
# a release wants and a test run does not, so it runs against a copy.
cp -R "$repo/npm" "$tmp/npm"
sh "$tmp/npm/stage.sh" "$repo/dist" "$version"

host_binary="$tmp/npm/platforms/$host_os-$host_arch/tenetlint"

for platform in darwin-arm64 darwin-x64 linux-arm64 linux-x64; do
	(cd "$tmp" && npm pack "$tmp/npm/platforms/$platform" >/dev/null 2>&1)
done
(cd "$tmp" && npm pack "$tmp/npm/tenetlint" >/dev/null 2>&1)

# The three foreign platform packages would fail npm's os/cpu check on this
# host, so only the host's own tarball is installed beside the entry package;
# --no-optional keeps npm from reaching for the other three in a registry.
npm install --prefix "$tmp" --no-optional --no-audit --no-fund \
	"$tmp/tenetlint-$version.tgz" \
	"$tmp/tenetlint-$host_os-$host_arch-$version.tgz" >/dev/null 2>&1

shim="$tmp/node_modules/.bin/tenetlint"

out=$("$shim" version)
direct=$("$host_binary" version)
echo "tenetlint version -> $out"

if [ "$out" != "$direct" ]; then
	echo "npm/test.sh: shim printed '$out', the binary itself '$direct'" >&2
	exit 1
fi

override_out=$(TENETLINT_BINARY="$host_binary" "$shim" version)
if [ "$override_out" != "$out" ]; then
	echo "npm/test.sh: TENETLINT_BINARY gave '$override_out', not '$out'" >&2
	exit 1
fi

if ! "$shim" --help >/dev/null; then
	echo "npm/test.sh: --help failed through the shim" >&2
	exit 1
fi

echo "npm package ok"
