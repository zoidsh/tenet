#!/bin/sh
# Usage: npm/stage.sh <dist-dir> <version>
set -eu

# The published name, in one place: the entry package, the scope of the four
# platform packages and the binary inside them all carry it.
NAME=tenetlint
SCOPE=@tenetlint
REPO=zoidsh/tenetlint

PLATFORMS="darwin-arm64 darwin-x64 linux-arm64 linux-x64"

if [ $# -ne 2 ]; then
	echo "usage: npm/stage.sh <dist-dir> <version>" >&2
	exit 2
fi

dist=$1
version=$2
npm_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

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
  "name": "$SCOPE/$platform",
  "version": "$version",
  "description": "The $NAME binary for $os_name on $cpu",
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
    "$NAME"
  ],
  "engines": {
    "node": ">=18"
  }
}
EOF
}

entry_manifest() {
	node -e '
		const fs = require("fs");
		const [file, version, scope, platforms] = process.argv.slice(1);
		const pkg = JSON.parse(fs.readFileSync(file, "utf8"));
		pkg.version = version;
		pkg.optionalDependencies = Object.fromEntries(
			platforms.split(" ").map((p) => [`${scope}/${p}`, version])
		);
		fs.writeFileSync(file, JSON.stringify(pkg, null, 2) + "\n");
	' "$npm_dir/$NAME/package.json" "$version" "$SCOPE" "$PLATFORMS"
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
	built=$(ls -d "$dist/${NAME}_${os}_${goarch}"* 2>/dev/null | head -n 1)
	if [ -z "$built" ] || [ ! -f "$built/$NAME" ]; then
		echo "stage.sh: no binary for $platform under $dist" >&2
		exit 1
	fi

	mkdir -p "$npm_dir/platforms/$platform"
	platform_manifest "$platform"
	cp "$built/$NAME" "$npm_dir/platforms/$platform/$NAME"
	chmod 755 "$npm_dir/platforms/$platform/$NAME"
	echo "staged $SCOPE/$platform $version from $built"
done
