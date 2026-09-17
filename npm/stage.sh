#!/bin/sh
# Usage: npm/stage.sh <dist-dir> <version>
set -eu

if [ $# -ne 2 ]; then
	echo "usage: npm/stage.sh <dist-dir> <version>" >&2
	exit 2
fi

dist=$1
version=$2
npm_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

set_version() {
	node -e '
		const fs = require("fs");
		const [file, version] = process.argv.slice(1);
		const pkg = JSON.parse(fs.readFileSync(file, "utf8"));
		pkg.version = version;
		for (const name of Object.keys(pkg.optionalDependencies || {})) {
			pkg.optionalDependencies[name] = version;
		}
		fs.writeFileSync(file, JSON.stringify(pkg, null, 2) + "\n");
	' "$1" "$version"
}

set_version "$npm_dir/tenetlint/package.json"

for platform in darwin-arm64 darwin-x64 linux-arm64 linux-x64; do
	goos=${platform%-*}
	arch=${platform#*-}
	case $arch in
	x64) goarch=amd64 ;;
	*) goarch=$arch ;;
	esac

	# goreleaser suffixes the build directory with the microarchitecture level
	# it targeted (_v1, _v8.0), which is not part of anything we name.
	built=$(ls -d "$dist/tenetlint_${goos}_${goarch}"* 2>/dev/null | head -n 1)
	if [ -z "$built" ] || [ ! -f "$built/tenetlint" ]; then
		echo "stage.sh: no binary for $platform under $dist" >&2
		exit 1
	fi

	set_version "$npm_dir/platforms/$platform/package.json"
	cp "$built/tenetlint" "$npm_dir/platforms/$platform/tenetlint"
	chmod 755 "$npm_dir/platforms/$platform/tenetlint"
	echo "staged @tenetlint/$platform $version from $built"
done
