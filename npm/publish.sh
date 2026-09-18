#!/bin/sh
# Usage: npm/publish.sh <version>
set -eu

PLATFORMS="darwin-arm64 darwin-x64 linux-arm64 linux-x64"

if [ $# -ne 1 ]; then
	echo "usage: npm/publish.sh <version>" >&2
	exit 2
fi

version=$1
npm_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

publish() {
	dir=$1
	name=$(node -p "require('$dir/package.json').name")
	# A publish run that failed partway through leaves some of these on the
	# registry, and npm rejects a version it already holds, so without this a
	# rerun dies on the first package instead of finishing the rest.
	if npm view "$name@$version" version >/dev/null 2>&1; then
		echo "skipping $name@$version, already published"
		return
	fi
	npm publish --provenance --access public "$dir"
}

# The platform packages go first: the entry package depends on them, and an
# install that reaches npm between the two would find them missing.
for platform in $PLATFORMS; do
	publish "$npm_dir/platforms/$platform"
done
publish "$npm_dir/tenet"
