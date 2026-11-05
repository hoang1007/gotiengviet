#!/usr/bin/env bash
# Build the GoTiengViet Debian package without modifying the source tree.
#
# Usage:
#   scripts/build-gotv-deb.sh [outdir]
#
# Env:
#   GOTV_RELEASE=22.04|24.04  package version gets a ~<release> suffix
#                             (26.04 and unset keep the public 0.8.5+gotv1-1 name)
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-"$root/dist"}
release=${GOTV_RELEASE:-}

case "$release" in
  ""|22.04|24.04|26.04) ;;
  *) echo "GOTV_RELEASE must be 22.04, 24.04, 26.04 or empty" >&2; exit 2 ;;
esac

for cmd in go cc pkg-config dpkg-buildpackage; do
  command -v "$cmd" >/dev/null || { echo "Missing build tool: $cmd" >&2; exit 1; }
done
pkg-config --exists gtk+-3.0 x11 xtst || { echo 'Missing GTK/X11 development libraries' >&2; exit 1; }
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/gotv-build.XXXXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir "$work/src"

version="0.8.5+gotv1-1"
if [[ -n "$release" && "$release" != 26.04 ]]; then
  version+="~${release}"
fi

# Include local changes, but never package Git internals or build outputs.
tar -C "$root" --exclude=./.git --exclude=./dist --exclude=./debian \
  --exclude='./ibus-engine-bamboo' -cf - . | tar -C "$work/src" -xf -
cp -a "$work/src/build/deb" "$work/src/debian"
cp "$work/src/LICENSE" "$work/src/debian/copyright"
{
  printf 'ibus-bamboo (%s) unstable; urgency=medium\n\n' "$version"
  printf '  * GoTiengViet fork.\n\n'
  printf ' -- GoTiengViet <dev@gotiengviet.local>  %s\n\n' "$(date -R)"
  cat "$work/src/debian/changelog"
} > "$work/changelog"
mv "$work/changelog" "$work/src/debian/changelog"

# Go may be installed outside dpkg; the required tools were checked above.
(cd "$work/src" && dpkg-buildpackage -b -us -uc -d)
arch=$(dpkg --print-architecture)
package="$work/ibus-bamboo_${version}_${arch}.deb"
[[ -f "$package" ]] || { echo "Package not found: $package" >&2; exit 1; }
# Publish under a '.' suffix: GitHub replaces '~' with '.' in release asset names.
outname="ibus-bamboo_${version//\~/.}_${arch}.deb"
cp "$package" "$out/$outname"
echo "Built: $out/$outname"
