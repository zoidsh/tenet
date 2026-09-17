#!/bin/sh
# Builds the binaries, stages the npm packages and installs them from packed
# tarballs, so the whole npm path is exercised without a release or a registry.
set -eu

NAME=tenetlint
BIN=tenet
ALIAS=tenetlint
PLATFORMS="darwin-arm64 darwin-x64 linux-arm64 linux-x64"

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

host_binary="$tmp/npm/platforms/$host_os-$host_arch/$BIN"

for platform in $PLATFORMS; do
	(cd "$tmp" && npm pack "$tmp/npm/platforms/$platform" >/dev/null 2>&1)
done
(cd "$tmp" && npm pack "$tmp/npm/$NAME" >/dev/null 2>&1)

# The three foreign platform packages would fail npm's os/cpu check on this
# host, so only the host's own tarball is installed beside the entry package;
# --omit=optional keeps npm from reaching for the other three in a registry.
npm install --prefix "$tmp" --omit=optional --no-audit --no-fund \
	"$tmp/$NAME-$version.tgz" \
	"$tmp/$NAME-$host_os-$host_arch-$version.tgz" >/dev/null 2>&1

direct=$("$host_binary" version)

# Both names are bins of the entry package, and both have to reach the binary.
for command in "$BIN" "$ALIAS"; do
	shim="$tmp/node_modules/.bin/$command"
	out=$("$shim" version)
	echo "$command version -> $out"
	if [ "$out" != "$direct" ]; then
		echo "npm/test.sh: $command printed '$out', the binary itself '$direct'" >&2
		exit 1
	fi
	if ! "$shim" --help >/dev/null; then
		echo "npm/test.sh: --help failed through $command" >&2
		exit 1
	fi
done

override_out=$(TENETLINT_BINARY="$host_binary" "$tmp/node_modules/.bin/$BIN" version)
if [ "$override_out" != "$direct" ]; then
	echo "npm/test.sh: TENETLINT_BINARY gave '$override_out', not '$direct'" >&2
	exit 1
fi

echo "npm package ok"
