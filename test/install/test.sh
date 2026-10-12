#!/bin/sh
# Checks of install.sh (repo root) as a user runs it: sh install.sh, configured by
# environment variables only. POSIX sh, runs on Linux and macOS.
#
# Contract under test:
#   LEETTEST_INSTALL_DIR  target directory, default $HOME/.local/bin
#   LEETTEST_VERSION      release tag to install (v0.1.0), default the latest release
#   LEETTEST_BASE_URL     release downloads root, default
#                         https://github.com/yhgrwav/leettest/releases; files are fetched from
#                         $LEETTEST_BASE_URL/download/<tag>/<file> (the layout of GitHub releases)
#   archive               leettest_<version without v>_<linux|darwin>_<amd64|arm64>.tar.gz,
#                         next to checksums.txt, as goreleaser publishes them
#   exit status           0 installed; 1 any refusal, with a message, nothing installed
#   missing tool          curl, tar, or both sha256sum and shasum absent: the script's own message
#                         names the tool ("curl is required"), not the shell's "not found"
#   unknown version       LEETTEST_VERSION with no release: exit 1, the tag named in the message,
#                         no fallback to the latest release
#   latest                resolved by asking https://github.com/yhgrwav/leettest/releases/latest,
#                         never the API or the list of releases (a pre-release can head the list)
#   temp files            a temp directory under $TMPDIR, removed on success and on refusal
#   checksum tool         sha256sum or shasum -a 256, whichever exists, either alone is enough
#   tools used            POSIX utilities, gzip (for tar), curl, tar, sha256sum or shasum
#
# Cases that need no network use a local fixture served through file://; cases against real
# releases are marked "network" and need `gh` authenticated (GH_TOKEN) for the expected tag.

set -u

root=$(cd "$(dirname "$0")/../.." && pwd)
script=${INSTALL_SH:-$root/install.sh}
sh_bin=$(command -v sh)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
failed=0
n=0

fail() {
	printf 'FAIL %s: %s\n' "$case_name" "$1"
	sed 's/^/    | /' "$work/out" 2>/dev/null | head -20
	failed=$((failed + 1))
}

start() {
	case_name=$1
	n=$((n + 1))
	: >"$work/out"
}

# run VAR=value ...: install.sh with exactly these variables on top of the current environment.
run() {
	env "$@" "$sh_bin" "$script" >"$work/out" 2>&1
	status=$?
}

expect_status() {
	[ "$status" -eq "$1" ] || fail "exit status $status, want $1"
}

expect_out() {
	grep -F -- "$1" "$work/out" >/dev/null || fail "output lacks: $1"
}

reject_out() {
	if grep -F -- "$1" "$work/out" >/dev/null; then fail "output has: $1"; fi
}

expect_no_binary() {
	[ ! -e "$1/leettest" ] || fail "$1/leettest was installed"
}

# shim_uname OS ARCH: a uname that reports these, first on PATH in $work/shim.
shim_uname() {
	rm -rf "$work/shim"
	mkdir -p "$work/shim"
	cat >"$work/shim/uname" <<EOF
#!/bin/sh
case "\$*" in
-m) echo $2 ;;
-s) echo $1 ;;
-sm | -ms) echo "$1 $2" ;;
*) echo $1 ;;
esac
EOF
	chmod +x "$work/shim/uname"
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi
}

# fixture TAG: archives for every platform in $work/fixture/download/TAG, each holding a fake
# `leettest` that prints "leettest TAG <os>_<arch>", plus LICENSE and README.md as the real ones do.
fixture() {
	tag=$1
	ver=${tag#v}
	dir=$work/fixture/download/$tag
	rm -rf "$work/fixture"
	mkdir -p "$dir"
	for pair in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
		pkg=$work/pkg/$pair
		mkdir -p "$pkg"
		printf '#!/bin/sh\necho "leettest %s %s"\n' "$tag" "$pair" >"$pkg/leettest"
		chmod +x "$pkg/leettest"
		echo license >"$pkg/LICENSE"
		echo readme >"$pkg/README.md"
		tar -czf "$dir/leettest_${ver}_$pair.tar.gz" -C "$pkg" leettest LICENSE README.md
	done
	(cd "$dir" && for f in leettest_*.tar.gz; do sha256 "$f"; done >checksums.txt)
	rm -rf "$work/pkg"
}

# limited_path [TOOL...]: sets $limited to a directory of symlinks to the only tools install.sh may
# use (see "tools used" above), without the named ones (curl, tar, sha256sum, shasum). Everything
# else on the real PATH stays out of reach.
limited_path() {
	d=$work/limited
	rm -rf "$d"
	mkdir -p "$d"
	for t in sh uname mktemp rm mkdir cp mv chmod cat grep sed tr awk head cut dirname basename \
		install ls id printf gzip curl tar sha256sum shasum; do
		skip=0
		for x in "$@"; do
			if [ "$x" = "$t" ]; then skip=1; fi
		done
		if [ "$skip" -eq 1 ]; then continue; fi
		p=$(command -v "$t" 2>/dev/null) || continue
		case "$p" in /*) ln -s "$p" "$d/$t" ;; esac
	done
	limited=$d
}

# spy_curl: replaces curl in $limited with a wrapper that logs its arguments to $work/curl.log and
# runs the real curl.
spy_curl() {
	real=$(command -v curl)
	rm -f "$limited/curl" "$work/curl.log"
	cat >"$limited/curl" <<EOF
#!/bin/sh
echo "\$*" >>"$work/curl.log"
exec "$real" "\$@"
EOF
	chmod +x "$limited/curl"
}

# reset_tmp: an empty $work/tmp to use as TMPDIR.
reset_tmp() {
	rm -rf "$work/tmp"
	mkdir -p "$work/tmp"
}

expect_tmp_empty() {
	if [ -n "$(ls -A "$work/tmp")" ]; then fail "temp files left in TMPDIR: $(ls -A "$work/tmp")"; fi
}

expect_log() {
	grep -F -- "$1" "$work/curl.log" >/dev/null 2>&1 || fail "curl was never asked for: $1"
}

reject_log() {
	if grep -F -- "$1" "$work/curl.log" >/dev/null 2>&1; then fail "curl was asked for: $1"; fi
}

# break_sums: the checksum of the current host's archive in the fixture no longer matches.
break_sums() {
	(
		cd "$work/fixture/download/v9.9.9" || exit 1
		sed "s/^[0-9a-f]\{8\}/00000000/" checksums.txt >checksums.bad && mv checksums.bad checksums.txt
	)
}

# --- each platform maps to its archive (offline) ---------------------------------------------

fixture v9.9.9
for combo in "Linux x86_64 linux_amd64" "Linux aarch64 linux_arm64" "Linux arm64 linux_arm64" \
	"Darwin x86_64 darwin_amd64" "Darwin arm64 darwin_arm64"; do
	p_os=${combo%% *}
	rest=${combo#* }
	p_arch=${rest%% *}
	p_pair=${rest#* }
	start "platform $p_os $p_arch"
	shim_uname "$p_os" "$p_arch"
	dir=$work/p/$p_os-$p_arch/bin
	run "PATH=$work/shim:$PATH" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
		"LEETTEST_BASE_URL=file://$work/fixture"
	expect_status 0
	if [ -x "$dir/leettest" ]; then
		got=$("$dir/leettest")
		[ "$got" = "leettest v9.9.9 $p_pair" ] || fail "installed binary says '$got', want 'leettest v9.9.9 $p_pair'"
		expect_out "leettest v9.9.9 $p_pair"
		if [ -e "$dir/LICENSE" ] || [ -e "$dir/README.md" ]; then fail "more than the binary was installed"; fi
	else
		fail "$dir/leettest is missing or not executable"
	fi
done

start "default directory is \$HOME/.local/bin"
mkdir -p "$work/home"
reset_tmp
run "HOME=$work/home" "TMPDIR=$work/tmp" LEETTEST_VERSION=v9.9.9 "LEETTEST_BASE_URL=file://$work/fixture"
expect_status 0
[ -x "$work/home/.local/bin/leettest" ] || fail "$work/home/.local/bin/leettest is missing"
expect_tmp_empty

host_arch=$(uname -m)
host_os=$(uname -s)
case "$host_os" in Darwin) os=darwin ;; *) os=linux ;; esac
case "$host_arch" in aarch64 | arm64) arch=arm64 ;; *) arch=amd64 ;; esac
file=leettest_9.9.9_${os}_${arch}.tar.gz

# --- refusals (offline) -----------------------------------------------------------------------

start "unsupported OS"
shim_uname FreeBSD x86_64
dir=$work/os/bin
run "PATH=$work/shim:$PATH" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_out "FreeBSD"
expect_out "https://github.com/yhgrwav/leettest/releases"
expect_out "go install"
expect_no_binary "$dir"

start "unsupported arch"
shim_uname Linux riscv64
dir=$work/arch/bin
run "PATH=$work/shim:$PATH" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_out "riscv64"
expect_out "https://github.com/yhgrwav/leettest/releases"
expect_out "go install"
expect_no_binary "$dir"

start "checksum mismatch"
fixture v9.9.9
break_sums
dir=$work/sum/bin
reset_tmp
run "TMPDIR=$work/tmp" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 "LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_out "checksum"
expect_no_binary "$dir"
expect_tmp_empty

start "archive missing from checksums.txt"
fixture v9.9.9
(
	cd "$work/fixture/download/v9.9.9" || exit 1
	grep -v -F "$file" checksums.txt >checksums.new
	mv checksums.new checksums.txt
)
dir=$work/nosum/bin
run "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 "LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_out "checksum"
expect_no_binary "$dir"

start "tampered archive"
fixture v9.9.9
printf 'x' >>"$work/fixture/download/v9.9.9/$file"
dir=$work/tamper/bin
run "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 "LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_out "checksum"
expect_no_binary "$dir"

fixture v9.9.9
for missing in curl tar sha; do
	start "missing tool: $missing"
	case "$missing" in
	sha) limited_path sha256sum shasum ;;
	*) limited_path "$missing" ;;
	esac
	dir=$work/miss-$missing/bin
	run "PATH=$limited" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
		"LEETTEST_BASE_URL=file://$work/fixture"
	expect_status 1
	case "$missing" in
	sha) expect_out "sha256sum" ;;
	*) expect_out "$missing" ;;
	esac
	reject_out "not found"
	expect_no_binary "$dir"
done

start "limited PATH is enough when no tool is missing"
limited_path
dir=$work/limited-ok/bin
run "PATH=$limited" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 0
[ -x "$dir/leettest" ] || fail "$dir/leettest is missing"

# Either checksum tool alone is enough, and each verifies: a script that calls only one of them
# fails the case for the other.
for only in shasum sha256sum; do
	case "$only" in
	shasum) other=sha256sum ;;
	*) other=shasum ;;
	esac
	if ! command -v "$only" >/dev/null 2>&1; then
		printf 'SKIP only %s: not on this host\n' "$only"
		continue
	fi

	start "only $only: installs"
	fixture v9.9.9
	limited_path "$other"
	dir=$work/only-$only/bin
	run "PATH=$limited" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
		"LEETTEST_BASE_URL=file://$work/fixture"
	expect_status 0
	[ -x "$dir/leettest" ] || fail "$dir/leettest is missing"

	start "only $only: refuses a wrong checksum"
	break_sums
	dir=$work/only-$only-bad/bin
	run "PATH=$limited" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
		"LEETTEST_BASE_URL=file://$work/fixture"
	expect_status 1
	expect_out "checksum"
	expect_no_binary "$dir"
done

# An unknown version is refused by name; nothing falls back to the latest release.
start "unknown version, offline"
fixture v9.9.9
dir=$work/nover/bin
run "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v0.0.0-nope "LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_no_binary "$dir"
if ! grep -F v0.0.0-nope "$work/out" | grep -v '^curl:' >/dev/null; then
	fail "no message of the script names v0.0.0-nope"
fi

# --- against real releases (network) ----------------------------------------------------------

start "network: latest release, directory not on PATH"
want=$(gh release view --repo yhgrwav/leettest --json tagName --jq .tagName)
dir=$work/latest/bin
limited_path
spy_curl
run "PATH=$limited" "LEETTEST_INSTALL_DIR=$dir"
expect_status 0
expect_log "https://github.com/yhgrwav/leettest/releases/latest"
reject_log "api.github.com"
if [ -x "$dir/leettest" ]; then
	got=$("$dir/leettest" -version)
	[ "$got" = "leettest $want" ] || fail "leettest -version says '$got', want 'leettest $want'"
	expect_out "leettest $want"
else
	fail "$dir/leettest is missing or not executable"
fi
expect_out "export PATH=\"$dir:\$PATH\""

start "network: pinned version, directory on PATH"
dir=$work/pinned/bin
mkdir -p "$dir"
run "PATH=$dir:$PATH" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v0.1.0
expect_status 0
if [ -x "$dir/leettest" ]; then
	got=$("$dir/leettest" -version)
	[ "$got" = "leettest v0.1.0" ] || fail "leettest -version says '$got', want 'leettest v0.1.0'"
else
	fail "$dir/leettest is missing or not executable"
fi
reject_out "export PATH"

start "network: unknown version"
dir=$work/nover-net/bin
run "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v0.0.0-nope
expect_status 1
expect_no_binary "$dir"

printf '%s cases, %s failed\n' "$n" "$failed"
[ "$failed" -eq 0 ]
