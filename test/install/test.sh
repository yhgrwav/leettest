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
#   exit status           0 installed; 1 any refusal, with a message, nothing installed (except a
#                         binary that does not run: it stays, see below)
#   missing tool          curl, tar, or both sha256sum and shasum absent: the script's own message
#                         names the tool ("curl is required"), not the shell's "not found"
#   unknown version       LEETTEST_VERSION with no release: exit 1, the tag named in the message,
#                         no fallback to the latest release
#   Rosetta               Darwin with uname -m x86_64 and `sysctl -n hw.optional.arm64` printing 1
#                         installs the arm64 archive; any other answer, or no sysctl, amd64
#   binary does not run   `leettest -version` failing after the copy: exit 1, "installed
#                         <dir>/leettest, but it does not run ..." (the file is there, and is
#                         left), no "Installed" line
#   failed install        a failing cp, chmod or mv, or TERM/INT after the copy, leaves no temp copy
#                         in the directory
#   latest                resolved by asking https://github.com/yhgrwav/leettest/releases/latest,
#                         never the API or the list of releases (a pre-release can head the list)
#   temp files            a temp directory under $TMPDIR, removed on success and on refusal
#   checksum tool         sha256sum or shasum -a 256, whichever exists, either alone is enough
#   tools used            POSIX utilities, gzip (for tar), curl, tar, sha256sum or shasum
#   cut-short download    the script is read from stdin (curl | sh); any prefix of it does nothing:
#                         no directory created, no file written, no temp files left. Exit status
#                         is not specified (a syntax error and a script that never reaches its
#                         last line both qualify).
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

# run_stdin FILE VAR=value ...: the script text comes on stdin, as in `curl | sh`.
run_stdin() {
	src=$1
	shift
	env "$@" "$sh_bin" <"$src" >"$work/out" 2>&1
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

# shim_sysctl VALUE: a sysctl in $work/shim (after shim_uname or shim_fail) that answers
# `sysctl -n hw.optional.arm64` with VALUE (0 or 1), or, for VALUE "fail", fails the way macOS on
# Intel does (unknown oid, status 1). Any other call fails.
shim_sysctl() {
	cat >"$work/shim/sysctl" <<EOF
#!/bin/sh
if [ "\$*" = "-n hw.optional.arm64" ]; then
	if [ "$1" = fail ]; then
		echo "sysctl: unknown oid 'hw.optional.arm64'" >&2
		exit 1
	fi
	echo $1
	exit 0
fi
echo "unexpected sysctl call: \$*" >&2
exit 1
EOF
	chmod +x "$work/shim/sysctl"
}

# shim_fail TOOL: an empty $work/shim holding only a TOOL that always fails with status 1.
shim_fail() {
	rm -rf "$work/shim"
	mkdir -p "$work/shim"
	printf '#!/bin/sh\necho "%s: refused" >&2\nexit 1\n' "$1" >"$work/shim/$1"
	chmod +x "$work/shim/$1"
}

# shim_cp_partial: an empty $work/shim holding a cp that writes the first bytes of the source to the
# target and fails with status 1.
shim_cp_partial() {
	rm -rf "$work/shim"
	mkdir -p "$work/shim"
	cat >"$work/shim/cp" <<'EOF'
#!/bin/sh
head -c 10 "$1" >"$2"
echo "cp: no space left on device" >&2
exit 1
EOF
	chmod +x "$work/shim/cp"
}

# shim_cp_term: an empty $work/shim holding a cp that copies for real, then sends TERM to its
# parent (the shell running install.sh) and succeeds.
shim_cp_term() {
	real_cp=$(command -v cp)
	rm -rf "$work/shim"
	mkdir -p "$work/shim"
	cat >"$work/shim/cp" <<EOF
#!/bin/sh
"$real_cp" "\$@" || exit 1
kill -TERM "\$PPID"
exit 0
EOF
	chmod +x "$work/shim/cp"
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi
}

# old_install DIR: an earlier install in DIR, whose `leettest` prints "leettest v0.0.1 old".
old_install() {
	mkdir -p "$1"
	printf '#!/bin/sh\necho "leettest v0.0.1 old"\n' >"$1/leettest"
	chmod +x "$1/leettest"
}

# expect_installed DIR WANT: DIR/leettest is executable and prints WANT.
expect_installed() {
	if [ -x "$1/leettest" ]; then
		got=$("$1/leettest" -version)
		[ "$got" = "$2" ] || fail "installed binary says '$got', want '$2'"
	else
		fail "$1/leettest is missing or not executable"
	fi
}

# expect_no_stray DIR: nothing but leettest in DIR (no temp copy left from the install).
expect_no_stray() {
	stray=$(find "$1" -mindepth 1 -maxdepth 1 ! -name leettest 2>/dev/null)
	[ -z "$stray" ] || fail "left in $1: $stray"
}

# fixture TAG [KIND]: archives for every platform in $work/fixture/download/TAG, each holding a fake
# `leettest` that prints "leettest TAG <os>_<arch>", plus LICENSE and README.md as the real ones do.
# KIND exit1: the fake exits 1 on every call; badinterp: its interpreter does not exist (what a
# binary built for another architecture or on a noexec mount looks like to sh).
fixture() {
	tag=$1
	ver=${tag#v}
	dir=$work/fixture/download/$tag
	rm -rf "$work/fixture"
	mkdir -p "$dir"
	for pair in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
		pkg=$work/pkg/$pair
		mkdir -p "$pkg"
		case ${2:-} in
		exit1) printf '#!/bin/sh\necho "cannot run here" >&2\nexit 1\n' >"$pkg/leettest" ;;
		badinterp) printf '#!/nonexistent/interpreter\n' >"$pkg/leettest" ;;
		*) printf '#!/bin/sh\necho "leettest %s %s"\n' "$tag" "$pair" >"$pkg/leettest" ;;
		esac
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
	shim_sysctl 0 # an Intel Mac, even when this host is an arm64 one
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

# --- Apple silicon under Rosetta (offline) ----------------------------------------------------
# An x86_64 shell on an arm64 Mac reports uname -m x86_64; `sysctl -n hw.optional.arm64` prints 1
# there. The native binary is the right one. Anything else, or no sysctl, keeps amd64.

for mode in 1 0 fail missing; do
	start "Darwin x86_64, sysctl $mode"
	shim_uname Darwin x86_64
	case "$mode" in
	missing)
		limited_path
		path=$work/shim:$limited
		want=darwin_amd64
		;;
	*)
		shim_sysctl "$mode"
		path=$work/shim:$PATH
		want=darwin_amd64
		if [ "$mode" = 1 ]; then want=darwin_arm64; fi
		;;
	esac
	dir=$work/rosetta-$mode/bin
	run "PATH=$path" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 "LEETTEST_BASE_URL=file://$work/fixture"
	expect_status 0
	expect_installed "$dir" "leettest v9.9.9 $want"
done

# Linux has no Rosetta: the answer of sysctl (which may exist there) does not matter.
start "Linux x86_64, sysctl 1"
shim_uname Linux x86_64
shim_sysctl 1
dir=$work/rosetta-linux/bin
run "PATH=$work/shim:$PATH" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 0
expect_installed "$dir" "leettest v9.9.9 linux_amd64"

# The real thing, where it can be had: an arm64 Mac (the macos-latest runner) pretending to be
# Intel, with its own sysctl.
if [ "$host_os" = Darwin ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null)" = 1 ]; then
	start "Darwin x86_64 shell on this arm64 Mac, real sysctl"
	shim_uname Darwin x86_64
	dir=$work/rosetta-real/bin
	run "PATH=$work/shim:$PATH" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
		"LEETTEST_BASE_URL=file://$work/fixture"
	expect_status 0
	expect_installed "$dir" "leettest v9.9.9 darwin_arm64"
elif [ "$host_os" = Darwin ]; then
	printf 'INFO real sysctl: hw.optional.arm64 = "%s" on %s %s\n' \
		"$(sysctl -n hw.optional.arm64 2>&1 | head -1)" "$host_os" "$host_arch"
fi

# --- installed, but does not run (offline) ----------------------------------------------------
# The file is in place, and the message says so; "Installed" is never printed for it. The
# binary stays where it is for the user to inspect.

for kind in exit1 badinterp; do
	start "installed binary does not run: $kind"
	fixture v9.9.9 "$kind"
	dir=$work/norun-$kind/bin
	reset_tmp
	run "TMPDIR=$work/tmp" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
		"LEETTEST_BASE_URL=file://$work/fixture"
	expect_status 1
	expect_out "installed $dir/leettest, but it does not run"
	reject_out "Installed"
	[ -e "$dir/leettest" ] || fail "the binary was deleted"
	expect_no_stray "$dir"
	expect_tmp_empty
done

# --- no stray temp file when the install itself fails (offline) -------------------------------

for tool in chmod mv; do
	start "$tool fails: nothing but a message is left"
	fixture v9.9.9
	shim_fail "$tool"
	dir=$work/stray-$tool/bin
	reset_tmp
	run "PATH=$work/shim:$PATH" "TMPDIR=$work/tmp" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
		"LEETTEST_BASE_URL=file://$work/fixture"
	expect_status 1
	expect_out "install.sh:"
	expect_no_binary "$dir"
	expect_no_stray "$dir"
	expect_tmp_empty
done

# A copy that dies halfway (disk full) leaves the first bytes in the temp file.
start "cp fails halfway: nothing but a message is left"
fixture v9.9.9
shim_cp_partial
dir=$work/stray-cp/bin
reset_tmp
run "PATH=$work/shim:$PATH" "TMPDIR=$work/tmp" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_out "install.sh:"
expect_no_binary "$dir"
expect_no_stray "$dir"
expect_tmp_empty

# Ctrl-C or TERM between the copy and the move: cleanup, not the failure branches, removes the temp
# copy.
start "TERM after the copy: nothing is left"
fixture v9.9.9
shim_cp_term
dir=$work/stray-term/bin
reset_tmp
run "PATH=$work/shim:$PATH" "TMPDIR=$work/tmp" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_no_binary "$dir"
expect_no_stray "$dir"
expect_tmp_empty

# The earlier install survives the failure and the cleanup of the temp copy (the stray-file check
# is red until the fix; the survival of the old binary is a guard).
start "chmod fails over an existing install: the old one stays"
fixture v9.9.9
shim_fail chmod
dir=$work/stray-old/bin
old_install "$dir"
run "PATH=$work/shim:$PATH" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_installed "$dir" "leettest v0.0.1 old"
expect_no_stray "$dir"

# --- guards (green before any fix): upgrade and the PATH hint, offline ------------------------

start "upgrade replaces an older install"
fixture v9.9.9
dir=$work/upgrade/bin
old_install "$dir"
reset_tmp
run "TMPDIR=$work/tmp" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 0
expect_installed "$dir" "leettest v9.9.9 ${os}_${arch}"
expect_out "leettest v9.9.9 ${os}_${arch}"
expect_no_stray "$dir"
expect_tmp_empty

start "checksum mismatch leaves the older install untouched"
fixture v9.9.9
break_sums
dir=$work/upgrade-bad/bin
old_install "$dir"
reset_tmp
run "TMPDIR=$work/tmp" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 1
expect_out "checksum"
expect_installed "$dir" "leettest v0.0.1 old"
expect_no_stray "$dir"
expect_tmp_empty

start "PATH hint, directory not on PATH"
fixture v9.9.9
dir=$work/hint/bin
run "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 "LEETTEST_BASE_URL=file://$work/fixture"
expect_status 0
expect_out "export PATH=\"$dir:\$PATH\""

start "PATH hint, directory on PATH"
dir=$work/hint-on/bin
mkdir -p "$dir"
run "PATH=$dir:$PATH" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
	"LEETTEST_BASE_URL=file://$work/fixture"
expect_status 0
reject_out "export PATH"
reject_out "not in your PATH"

# A download cut short must do nothing: the script runs from stdin, so a prefix of it is what sh
# sees. Cut at every tenth of its size (mid-line too) and with only the last line removed.
start "whole script from stdin installs"
fixture v9.9.9
dir=$work/stdin/bin
run_stdin "$script" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 "LEETTEST_BASE_URL=file://$work/fixture"
expect_status 0
[ -x "$dir/leettest" ] || fail "$dir/leettest is missing"

size=$(wc -c <"$script" | tr -d ' ')
lines=$(wc -l <"$script" | tr -d ' ')
for cut in 10 20 30 40 50 60 70 80 90 last-line; do
	start "cut short: $cut"
	case "$cut" in
	last-line) head -n $((lines - 1)) "$script" >"$work/cut.sh" ;;
	*) head -c $((size * cut / 100)) "$script" >"$work/cut.sh" ;;
	esac
	dir=$work/cut-$cut/bin
	reset_tmp
	run_stdin "$work/cut.sh" "TMPDIR=$work/tmp" "LEETTEST_INSTALL_DIR=$dir" LEETTEST_VERSION=v9.9.9 \
		"LEETTEST_BASE_URL=file://$work/fixture"
	if [ -e "$dir" ] || [ -e "$work/cut-$cut" ]; then fail "$dir was created"; fi
	expect_tmp_empty
done

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
