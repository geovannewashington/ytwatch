#!/bin/sh
# Installs ytwatch.
#
#   curl -fsSL https://github.com/geovannewashington/ytwatch/releases/latest/download/install.sh | sh
#
# What it does, in order:
#   1. downloads the ytwatch release for this machine from GitHub,
#   2. checks the download against the release's SHA-256 checksum,
#   3. installs the ytwatch program in ~/.local/bin,
#   4. shows the command that installs the programs ytwatch needs (mpv, yt-dlp,
#      ...) and asks before running it.
# It uses sudo only inside that last command, and only if you say yes.
#
# Options:
#   -y, --yes    run the command for the missing programs without asking
#   --no-deps    only install ytwatch
#   -h, --help   show this text
#
# Environment:
#   VERSION             a release such as v0.1.0 (default: the latest)
#   YTWATCH_INSTALL_DIR where to put ytwatch (default: ~/.local/bin)

set -eu

REPO="geovannewashington/ytwatch"
# The tests point this at a folder on disk (a file:// address).
RELEASES="${YTWATCH_RELEASES_URL:-https://github.com/$REPO/releases}"
INSTALL_DIR="${YTWATCH_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
fail() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

usage() {
	sed -n '2,/^$/p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//' || true
}

YES=0
DEPS=1
for arg in "$@"; do
	case $arg in
	-y | --yes) YES=1 ;;
	--no-deps) DEPS=0 ;;
	-h | --help)
		usage
		exit 0
		;;
	*) fail "unknown option: $arg (try --help)" ;;
	esac
done

# --- this machine ---------------------------------------------------------

[ "$(uname -s)" = "Linux" ] || fail "ytwatch is for Linux; this system is $(uname -s)."

case $(uname -m) in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*) fail "no release for the $(uname -m) processor (x86-64 and ARM64 are supported)." ;;
esac

command -v curl >/dev/null 2>&1 || fail "curl is needed: install it with your package manager and run this again."

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		fail "sha256sum is needed to check the download."
	fi
}

# Downloads are only accepted over HTTPS (or from a folder on disk, for tests).
fetch() {
	case $1 in
	https://*) curl --proto '=https' --tlsv1.2 -fsSL "$1" -o "$2" ;;
	file://*) curl -fsSL "$1" -o "$2" ;;
	*) fail "refusing to download from $1" ;;
	esac
}

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

# --- download and check ---------------------------------------------------

if [ -n "${VERSION:-}" ]; then
	BASE="$RELEASES/download/$VERSION"
else
	BASE="$RELEASES/latest/download"
fi

say "Downloading ytwatch for linux/$ARCH..."

fetch "$BASE/checksums.txt" "$TMP/checksums.txt" ||
	fail "could not download the release list from $BASE (is there a release${VERSION:+ $VERSION}?)"

# A line of checksums.txt is: <sha256>  <file name>
LINE=$(grep "_linux_${ARCH}\.tar\.gz\$" "$TMP/checksums.txt" | head -n 1 || true)
[ -n "$LINE" ] || fail "this release has no download for linux/$ARCH."

WANT=$(printf '%s\n' "$LINE" | awk '{print $1}')
NAME=$(printf '%s\n' "$LINE" | awk '{print $2}')

# The name goes into a file path and a web address: allow plain characters only.
case $NAME in
*[!A-Za-z0-9._-]* | "") fail "unexpected file name in checksums.txt: $NAME" ;;
esac

fetch "$BASE/$NAME" "$TMP/$NAME" || fail "could not download $NAME"

GOT=$(sha256 "$TMP/$NAME")
[ "$GOT" = "$WANT" ] || fail "the checksum of $NAME does not match (wanted $WANT, got $GOT). Nothing was installed."

say "Checksum ok."

# --- install ---------------------------------------------------------------

# Only the program is taken out of the archive.
tar -xzf "$TMP/$NAME" -C "$TMP" ytwatch || fail "$NAME does not contain ytwatch."

mkdir -p "$INSTALL_DIR"
# Written under another name and then renamed, so a running ytwatch is not
# overwritten halfway.
install -m 755 "$TMP/ytwatch" "$INSTALL_DIR/ytwatch.new"
mv -f "$INSTALL_DIR/ytwatch.new" "$INSTALL_DIR/ytwatch"

say "Installed $("$INSTALL_DIR/ytwatch" --version) in $INSTALL_DIR"

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	say ""
	say "$INSTALL_DIR is not in your PATH. Add it:"
	case ${SHELL:-} in
	*/fish) say "  fish_add_path $INSTALL_DIR" ;;
	*) say "  export PATH=\"$INSTALL_DIR:\$PATH\"    # put this line in your shell's startup file" ;;
	esac
	;;
esac

# --- the programs ytwatch needs --------------------------------------------

if [ "$DEPS" = 1 ]; then
	# ytwatch itself knows what is missing and how to install it here.
	CMD=$("$INSTALL_DIR/ytwatch" doctor --install-command 2>/dev/null || true)

	if [ -n "$CMD" ]; then
		say ""
		say "ytwatch needs programs that are not installed. This installs them:"
		say "  $CMD"

		RUN=0
		if [ "$YES" = 1 ]; then
			RUN=1
		elif (: </dev/tty) 2>/dev/null; then
			# The script itself may be coming through a pipe, so ask on the terminal.
			printf 'Run it now? [y/N] '
			read -r ANSWER </dev/tty || ANSWER=""
			case $ANSWER in
			y | Y | yes | YES) RUN=1 ;;
			esac
		fi

		if [ "$RUN" = 1 ]; then
			sh -c "$CMD" || say "That command failed. Fix the error above and run it again."
		else
			say "Run that command yourself when you are ready."
		fi
	fi
fi

say ""
say "Checking what ytwatch finds on this system:"
say ""
"$INSTALL_DIR/ytwatch" doctor || true
