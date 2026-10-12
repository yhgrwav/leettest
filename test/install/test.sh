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

# limited_path [curl|tar|sha]: sets $limited to a directory of symlinks to the only tools install.sh
# may use (POSIX utilities, gzip for tar, curl, tar, sha256sum or shasum), without the named one; "sha" leaves out
# both checksum tools. Everything else on the real PATH stays out of reach.
limited_path() {
	d=$work/limited
	rm -rf "$d"
	mkdir -p "$d"
	for t in sh uname mktemp rm mkdir cp mv chmod cat grep sed tr awk head cut dirname basename \
		install ls id printf gzip curl tar sha256sum shasum; do
		case "${1:-}:$t" in
		curl:curl | tar:tar | sha:sha256sum | sha:shasum) continue ;;
		esac
		p=$(command -v "$t" 2>/dev/null) || continue
		case "$p" in /*) ln -s "$p" "$d/$t" ;; esac
	done
	limited=$d
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
run "HOME=$work/home" LEETTEST_VERSION=v9.9.9 "LEETTEST_BASE_URL=file://$work/fixture"
expect_status 0
[ -x "$work/home/.local/bin/leettest" ] || fail "$work/home/.local/bin/leettest is missing"

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
host_arch=$(uname -m)
host_os=$(uname -s)
case "$host_os" in Darwin) os=darwin ;; *) os=linux ;; esac
case "$host_arch" in aarch64 | arm64) arch=arm64 ;; *) arch=amd64 ;; esac
file=leettest_9.9.9_${os}_${arch}.tar.gz
(
	cd "$work/fixture/download/v9.9.9" || exit 1
	sed "s/^[0-9a-f]\{8\}/00000000/" checksums.txt >checksums.bad && mv checksums.bad checksums.txt
)
dir=$work/sum/bin
run "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 "LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_out "checksum"
expect_no_binary "$dir"

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
	limited_path "$missing"
	dir=$work/miss-$missing/bin
	run "PATH=$limited" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
		"LEETTEST_BASE_URL=file://$work/fixture"
	expect_status 1
	case "$missing" in
	sha) expect_out "sha256sum" ;;
	*) expect_out "$missing" ;;
	esac
	expect_no_binary "$dir"
done

start "limited PATH is enough when no tool is missing"
limited_path
dir=$work/limited-ok/bin
run "PATH=$limited" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 0
[ -x "$dir/leettest" ] || fail "$dir/leettest is missing"

# --- against real releases (network) ----------------------------------------------------------

start "network: latest release, directory not on PATH"
want=$(gh release view --repo yhgrwav/leettest --json tagName --jq .tagName)
dir=$work/latest/bin
run "LEETTEST_INSTALL_DIR=$dir"
expect_status 0
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

printf '%s cases, %s failed\n' "$n" "$failed"
[ "$failed" -eq 0 ]
