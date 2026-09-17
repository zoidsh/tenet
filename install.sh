#!/bin/sh
# curl -fsSL https://raw.githubusercontent.com/zoidsh/tenetlint/main/install.sh | sh
set -eu

REPO=zoidsh/tenetlint
INSTALL_DIR=${TENETLINT_INSTALL_DIR:-$HOME/.local/bin}

die() {
	echo "tenetlint: $1" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is needed and was not found"
}

detect_os() {
	case $(uname -s) in
	Darwin) echo darwin ;;
	Linux) echo linux ;;
	*) die "no release for $(uname -s); build from source with go install" ;;
	esac
}

detect_arch() {
	case $(uname -m) in
	x86_64 | amd64) echo amd64 ;;
	arm64 | aarch64) echo arm64 ;;
	*) die "no release for $(uname -m); build from source with go install" ;;
	esac
}

latest_version() {
	# The redirect from /releases/latest names the tag, which costs no API quota
	# and needs no token, unlike the releases API.
	tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
		"https://github.com/$REPO/releases/latest" | sed 's:.*/tag/::')
	[ -n "$tag" ] || die "could not find the latest release"
	echo "${tag#v}"
}

archive_name() {
	echo "tenetlint_$1_$2_$3.tar.gz"
}

checksum() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "neither sha256sum nor shasum is available"
	fi
}

on_path() {
	case ":$PATH:" in
	*":$1:"*) return 0 ;;
	*) return 1 ;;
	esac
}

main() {
	need curl
	need tar

	os=$(detect_os)
	arch=$(detect_arch)
	# A dry run resolves no version of its own, so that it stays offline and
	# works before the first release exists.
	if [ -n "${TENETLINT_VERSION:-}" ]; then
		version=${TENETLINT_VERSION#v}
	elif [ "${TENETLINT_DRY_RUN:-}" = 1 ]; then
		version=LATEST
	else
		version=$(latest_version)
	fi
	archive=$(archive_name "$version" "$os" "$arch")
	base="https://github.com/$REPO/releases/download/v$version"

	if [ "${TENETLINT_DRY_RUN:-}" = 1 ]; then
		echo "os          $os"
		echo "arch        $arch"
		echo "version     $version"
		echo "archive     $archive"
		echo "url         $base/$archive"
		echo "checksums   $base/checksums.txt"
		echo "install dir $INSTALL_DIR"
		exit 0
	fi

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT

	curl -fsSL "$base/$archive" -o "$tmp/$archive" ||
		die "could not download $base/$archive"
	curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" ||
		die "could not download $base/checksums.txt"

	want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
	[ -n "$want" ] || die "checksums.txt does not list $archive"
	got=$(checksum "$tmp/$archive")
	[ "$want" = "$got" ] || die "checksum mismatch for $archive: expected $want, got $got"

	tar -xzf "$tmp/$archive" -C "$tmp" tenetlint
	mkdir -p "$INSTALL_DIR"
	install -m 755 "$tmp/tenetlint" "$INSTALL_DIR/tenetlint" 2>/dev/null || {
		cp "$tmp/tenetlint" "$INSTALL_DIR/tenetlint"
		chmod 755 "$INSTALL_DIR/tenetlint"
	}

	echo "tenetlint $version installed to $INSTALL_DIR/tenetlint"
	if ! on_path "$INSTALL_DIR"; then
		echo "$INSTALL_DIR is not on your PATH; add it with:"
		echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
	fi
}

main
