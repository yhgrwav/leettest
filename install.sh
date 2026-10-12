#!/bin/sh
# Installs the leettest binary from a GitHub release (Linux and macOS):
#
#   curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
#
# Settings are environment variables; put them before sh, not before curl:
#
#   curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | LEETTEST_VERSION=v0.2.0 sh
#
# LEETTEST_INSTALL_DIR  where to put it (default $HOME/.local/bin, no sudo)
# LEETTEST_VERSION      release tag, e.g. v0.2.0 (default: the latest release)
# LEETTEST_BASE_URL     releases URL (default https://github.com/yhgrwav/leettest/releases)
#
# Everything that acts is inside main, called on the last line: a download cut short
# is a syntax error or a script that never calls main, and does nothing.
set -eu

tmp=
staged=

die() {
	echo "install.sh: $*" >&2
	exit 1
}

cleanup() {
	if [ -n "$tmp" ]; then rm -rf "$tmp"; fi
	if [ -n "$staged" ]; then rm -f "$staged"; fi
}

have() {
	command -v "$1" >/dev/null 2>&1
}

main() {
	releases=https://github.com/yhgrwav/leettest/releases
	base=${LEETTEST_BASE_URL:-$releases}
	dir=${LEETTEST_INSTALL_DIR:-${HOME:-}/.local/bin}

	trap cleanup EXIT
	trap 'exit 1' HUP INT TERM

	have curl || die "curl is required"
	have tar || die "tar is required"
	if have sha256sum; then
		sha256() { sha256sum "$1"; }
	elif have shasum; then
		sha256() { shasum -a 256 "$1"; }
	else
		die "sha256sum or shasum is required to verify the download"
	fi

	os=$(uname -s)
	arch=$(uname -m)
	unsupported="unsupported system: $os $arch. Download an archive from $releases or run: go install github.com/yhgrwav/leettest/cmd/leettest@latest"
	case $os in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "$unsupported" ;;
	esac
	case $arch in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "$unsupported" ;;
	esac
	# A shell under Rosetta on an Apple silicon Mac reports x86_64; the native binary is the right one.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ] && have sysctl &&
		[ "$(sysctl -n hw.optional.arm64 2>/dev/null)" = 1 ]; then
		arch=arm64
	fi

	tag=${LEETTEST_VERSION:-}
	if [ -z "$tag" ]; then
		# The latest release is the newest one that is neither a draft nor a pre-release.
		url=$(curl -fsSLI --retry 3 -o /dev/null -w '%{url_effective}' "$base/latest") ||
			die "cannot find the latest release at $base/latest"
		tag=${url##*/}
		case $tag in
		v[0-9]*) ;;
		*) die "cannot read a release tag from $url" ;;
		esac
	fi

	file=leettest_${tag#v}_${os}_${arch}.tar.gz
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/leettest-install.XXXXXX")

	for name in "$file" checksums.txt; do
		curl -fsSL --retry 3 -o "$tmp/$name" "$base/download/$tag/$name" ||
			die "cannot download $base/download/$tag/$name (is $tag a release?)"
	done

	want=$(awk -v f="$file" '$2 == f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || die "no checksum for $file in checksums.txt"
	got=$(sha256 "$tmp/$file" | awk '{ print $1 }')
	[ "$got" = "$want" ] || die "checksum mismatch for $file: got $got, want $want"

	tar -xzf "$tmp/$file" -C "$tmp" leettest || die "cannot unpack $file"
	mkdir -p "$dir" || die "cannot create $dir"
	staged=$dir/.leettest.$$
	cp "$tmp/leettest" "$staged" || die "cannot write to $dir"
	chmod 755 "$staged" || die "cannot make $staged executable"
	mv -f "$staged" "$dir/leettest" || die "cannot write $dir/leettest"

	"$dir/leettest" -version ||
		die "installed $dir/leettest, but it does not run (a noexec mount, or a binary for another architecture?)"
	echo "Installed $dir/leettest"

	case ":${PATH:-}:" in
	*":$dir:"*) ;;
	*) echo "$dir is not in your PATH. Add it to your shell profile: export PATH=\"$dir:\$PATH\"" ;;
	esac
}

main "$@"
