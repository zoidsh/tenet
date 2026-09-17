#!/bin/sh
# Usage: npm/stage.sh <dist-dir> <version>
set -eu

# The published names, in one place: NAME is the directory and the archive
# stem, PKG is the entry package and the stem of the four platform packages,
# BIN is the command the platform packages carry.
NAME=tenet
BIN=tenet
PKG=@zoidsh/tenet
REPO=zoidsh/tenet

PLATFORMS="darwin-arm64 darwin-x64 linux-arm64 linux-x64"

if [ $# -ne 2 ]; then
	echo "usage: npm/stage.sh <dist-dir> <version>" >&2
	exit 2
fi

dist=$1
version=$2
npm_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

platform_manifest() {
	platform=$1
	os=${platform%-*}
	cpu=${platform#*-}
	case $os in
	darwin) os_name="macOS" ;;
	*) os_name="Linux" ;;
	esac

	cat >"$npm_dir/platforms/$platform/package.json" <<EOF
{
  "name": "$PKG-$platform",
  "version": "$version",
  "description": "The $BIN binary for $os_name on $cpu",
  "license": "MIT",
  "repository": {
    "type": "git",
    "url": "git+https://github.com/$REPO.git"
  },
  "homepage": "https://github.com/$REPO",
  "os": [
    "$os"
  ],
  "cpu": [
    "$cpu"
  ],
  "files": [
    "$BIN"
  ],
  "engines": {
    "node": ">=18"
  }
}
EOF
}

entry_manifest() {
	# shellcheck disable=SC2016 # the $ and ${} below are the node script's, not the shell's
	node -e '
		const fs = require("fs");
		const [file, version, prefix, platforms] = process.argv.slice(1);
		const pkg = JSON.parse(fs.readFileSync(file, "utf8"));
		pkg.version = version;
		pkg.optionalDependencies = Object.fromEntries(
			platforms.split(" ").map((p) => [`${prefix}-${p}`, version])
		);
		fs.writeFileSync(file, JSON.stringify(pkg, null, 2) + "\n");
	' "$npm_dir/$NAME/package.json" "$version" "$PKG" "$PLATFORMS"
}

entry_manifest

for platform in $PLATFORMS; do
	os=${platform%-*}
	cpu=${platform#*-}
	case $cpu in
	x64) goarch=amd64 ;;
	*) goarch=$cpu ;;
	esac

	# goreleaser suffixes the build directory with the microarchitecture level
	# it targeted (_v1, _v8.0), which is not part of anything we name.
	built=
	for candidate in "$dist/${NAME}_${os}_${goarch}"*; do
		if [ -e "$candidate" ]; then
			built=$candidate
			break
		fi
	done
	if [ -z "$built" ] || [ ! -f "$built/$BIN" ]; then
		echo "stage.sh: no binary for $platform under $dist" >&2
		exit 1
	fi

	mkdir -p "$npm_dir/platforms/$platform"
	platform_manifest "$platform"
	cp "$built/$BIN" "$npm_dir/platforms/$platform/$BIN"
	chmod 755 "$npm_dir/platforms/$platform/$BIN"
	echo "staged $PKG-$platform $version from $built"
done
