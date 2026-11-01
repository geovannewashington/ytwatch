#!/bin/sh
# Tests install.sh against a folder that looks like a GitHub release.
#
#   scripts/test-install.sh          builds a test release from the source
#   scripts/test-install.sh dist     uses the archives and checksums.txt that
#                                    GoReleaser put in dist/
#
# Nothing is downloaded and nothing outside a temporary folder is changed.

set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

PASSED=0
check() { # check <description> <command...>
	DESC=$1
	shift
	if "$@"; then
		PASSED=$((PASSED + 1))
		printf 'ok    %s\n' "$DESC"
	else
		printf 'FAIL  %s\n' "$DESC" >&2
		exit 1
	fi
}

case $(uname -m) in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*) echo "unsupported machine: $(uname -m)" >&2; exit 1 ;;
esac

# --- a release folder: latest/download/ and download/<tag>/ ----------------

DIST=${1:-}
if [ -z "$DIST" ]; then
	DIST=$WORK/dist
	mkdir -p "$DIST"
	(cd "$ROOT" && go build -ldflags "-X main.version=0.0.0-test" -o "$WORK/ytwatch" ./cmd/ytwatch)
	tar -czf "$DIST/ytwatch_0.0.0-test_linux_$ARCH.tar.gz" -C "$WORK" ytwatch
	(cd "$DIST" && sha256sum ./*.tar.gz | sed 's| \./| |' >checksums.txt)
fi

release() { # release <folder>: lay out the files like GitHub does
	mkdir -p "$1/latest/download" "$1/download/v9.9.9"
	cp "$DIST"/*.tar.gz "$DIST"/checksums.txt "$1/latest/download/"
	cp "$DIST"/*.tar.gz "$DIST"/checksums.txt "$1/download/v9.9.9/"
}
release "$WORK/release"

# Without a terminal the script cannot ask questions, as in a cron job or CI.
# setsid takes the terminal away. SETSID is the same thing for the commands
# that are written as text for sh -c.
SETSID=""
command -v setsid >/dev/null 2>&1 && SETSID="setsid -w"

detached() {
	if [ -n "$SETSID" ]; then
		setsid -w "$@"
	else
		"$@"
	fi
}

install_sh() { # install_sh <release folder> <install dir> <options...>
	REL=$1
	DIR=$2
	shift 2
	detached env YTWATCH_RELEASES_URL="file://$REL" YTWATCH_INSTALL_DIR="$DIR" sh "$ROOT/install.sh" "$@"
}

# --- tests -----------------------------------------------------------------

BIN1=$WORK/bin1
check "installs the latest release" install_sh "$WORK/release" "$BIN1" --no-deps >"$WORK/out1" 2>&1
check "the program is executable" test -x "$BIN1/ytwatch"
check "the program reports its version" "$BIN1/ytwatch" --version
check "it says the checksum was checked" grep -q "Checksum ok" "$WORK/out1"
check "no temporary file is left behind" test ! -e "$BIN1/ytwatch.new"

BIN2=$WORK/bin2
check "installs a chosen version" detached env VERSION=v9.9.9 YTWATCH_RELEASES_URL="file://$WORK/release" \
	YTWATCH_INSTALL_DIR="$BIN2" sh "$ROOT/install.sh" --no-deps >/dev/null 2>&1
check "that version is installed" test -x "$BIN2/ytwatch"

# A damaged download must fail and install nothing.
release "$WORK/bad"
for f in "$WORK"/bad/latest/download/*.tar.gz; do printf 'damage' >>"$f"; done
BIN3=$WORK/bin3
check "a damaged download is refused" sh -c "! YTWATCH_RELEASES_URL=file://$WORK/bad YTWATCH_INSTALL_DIR=$BIN3 $SETSID sh '$ROOT/install.sh' --no-deps >$WORK/out3 2>&1"
check "the error mentions the checksum" grep -q "checksum" "$WORK/out3"
check "nothing was installed" test ! -e "$BIN3/ytwatch"

check "an unknown option is refused" sh -c "! sh '$ROOT/install.sh' --nope >/dev/null 2>&1"
check "a missing release is reported" sh -c "! YTWATCH_RELEASES_URL=file://$WORK/none YTWATCH_INSTALL_DIR=$WORK/bin4 $SETSID sh '$ROOT/install.sh' --no-deps >/dev/null 2>&1"

# --- the step for missing programs, with a stand-in for ytwatch -------------

STUB=$WORK/stub
mkdir -p "$STUB/dist"
cat >"$STUB/ytwatch" <<'STUBEOF'
#!/bin/sh
case "$*" in
--version) echo "ytwatch 0.0.0-stub" ;;
"doctor --install-command") echo 'touch "$STUB_MARKER"' ;;
*) echo "stub doctor" ;;
esac
STUBEOF
chmod +x "$STUB/ytwatch"
tar -czf "$STUB/dist/ytwatch_0.0.0-stub_linux_$ARCH.tar.gz" -C "$STUB" ytwatch
(cd "$STUB/dist" && sha256sum ./*.tar.gz | sed 's| \./| |' >checksums.txt)
mkdir -p "$WORK/stubrel/latest/download"
cp "$STUB"/dist/* "$WORK/stubrel/latest/download/"

export STUB_MARKER="$WORK/marker"
rm -f "$STUB_MARKER"
check "without a terminal it does not run the install command" \
	install_sh "$WORK/stubrel" "$WORK/bin5" >"$WORK/out5" 2>&1
# The text is meant literally: the stand-in prints it with the $ in place.
# shellcheck disable=SC2016
check "it shows the command instead" grep -q 'touch "$STUB_MARKER"' "$WORK/out5"
check "and the command was not run" test ! -e "$STUB_MARKER"

check "--yes runs the install command" install_sh "$WORK/stubrel" "$WORK/bin6" --yes >/dev/null 2>&1
check "and the command ran" test -e "$STUB_MARKER"

rm -f "$STUB_MARKER"
check "--no-deps skips the install command" install_sh "$WORK/stubrel" "$WORK/bin7" --yes --no-deps >/dev/null 2>&1
check "and the command did not run" test ! -e "$STUB_MARKER"

printf '\n%d checks passed\n' "$PASSED"
