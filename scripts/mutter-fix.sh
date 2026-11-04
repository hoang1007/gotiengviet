#!/usr/bin/env bash
# mutter-fix.sh — backport upstream mutter fix 570de65 only where it is needed.
#
# The bug exists in mutter 50.0–50.3: clutter_input_method_forward_key() passes
# a NULL source device and the CLUTTER_IS_INPUT_DEVICE guard drops the event.
# Older Ubuntu branches (22.04 = mutter 42.x, 24.04 = mutter 46.x) pass a real
# keyboard device, so they do NOT need this patch. mutter >= 50.4 contains the
# upstream fix.
#
# Usage:
#   mutter-fix.sh status    report OS/mutter state and whether a patch is needed
#   mutter-fix.sh doctor    alias for status
#   mutter-fix.sh build     fetch source, apply patch, build .debs (no install)
#   mutter-fix.sh install   build + install, then prompt for logout
#   mutter-fix.sh restore   reinstall the original mutter packages
#
# Env: FORCE=1                 skip the Ubuntu distro check
#      WORK_DIR=...            build directory (default /var/tmp/gotv-mutter-fix)
#      LOG_FILE=...            log file
#      OS_RELEASE_FILE=...     alternate os-release (testing)
#      GOTV_MUTTER_VERSION=... override detected mutter-common version (testing)
#      GOTV_ABI_PACKAGE=...    override detected libmutter-*-0 package (testing)
set -euo pipefail

NAME=mutter-fix
WORK_DIR=${WORK_DIR:-/var/tmp/gotv-mutter-fix}
LOG_FILE=${LOG_FILE:-/tmp/gotv-mutter-fix.${USER:-$(id -u)}.log}
MARKER=${GOTV_MARKER:-/usr/share/gotv/mutter-fix.info}
PKG=mutter
ORIG_DEBS="$WORK_DIR/original"

OS_RELEASE_FILE=${OS_RELEASE_FILE:-/etc/os-release}
if [[ -r "$OS_RELEASE_FILE" ]]; then
  # shellcheck disable=SC1090
  . "$OS_RELEASE_FILE"
fi
ID=${ID:-}
VERSION_ID=${VERSION_ID:-}
PRETTY_NAME=${PRETTY_NAME:-${ID:-unknown} ${VERSION_ID:-}}
CODENAME=${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}

MUTTER_VER=${GOTV_MUTTER_VERSION:-$(dpkg-query -W -f='${Version}' mutter-common 2>/dev/null || true)}
MUTTER_UPSTREAM=${MUTTER_VER#*:}
MUTTER_UPSTREAM=${MUTTER_UPSTREAM%%-*}

if [[ -n ${GOTV_ABI_PACKAGE:-} ]]; then
  ABI_PKG=$GOTV_ABI_PACKAGE
else
  ABI_PKG=$(dpkg-query -W -f='${db:Status-Status} ${Package}\n' 'libmutter-*-0' 2>/dev/null \
            | awk '$1 == "installed" { print $2; exit }')
fi
ABI_SERIES=${ABI_PKG#libmutter-}
ABI_SERIES=${ABI_SERIES%-0}

PATCH_FILE=$(cat <<'EOF'
--- a/clutter/clutter/clutter-event.c
+++ b/clutter/clutter/clutter-event.c
@@ -1848,7 +1848,8 @@ clutter_event_key_new (ClutterEventType     type,
   g_return_val_if_fail (type == CLUTTER_KEY_PRESS ||
                         type == CLUTTER_KEY_RELEASE ||
                         type == CLUTTER_KEY_STATE, NULL);
-  g_return_val_if_fail (CLUTTER_IS_INPUT_DEVICE (source_device), NULL);
+  g_return_val_if_fail ((flags & CLUTTER_EVENT_FLAG_INPUT_METHOD) != 0 ||
+                        CLUTTER_IS_INPUT_DEVICE (source_device), NULL);
 
   event = clutter_event_new (type);
EOF
)

log() { printf '[%s] %s\n' "$NAME" "$*" | tee -a "$LOG_FILE" 2>/dev/null || printf '[%s] %s\n' "$NAME" "$*"; }
die() { printf '[%s] ERROR: %s\n' "$NAME" "$*" | tee -a "$LOG_FILE" >&2 2>/dev/null || printf '[%s] ERROR: %s\n' "$NAME" "$*" >&2; exit 1; }

need_tools() {
  local t
  for t in "$@"; do
    command -v "$t" >/dev/null 2>&1 || die "missing tool: $t (sudo apt-get install $t)"
  done
}

package_installed() {
  [[ $(dpkg-query -W -f='${db:Status-Status}' "$1" 2>/dev/null || true) == installed ]]
}

fix_state() {
  if [[ -z "$MUTTER_VER" ]]; then
    echo NO_MUTTER
  elif [[ "$MUTTER_VER" == *+gotv* ]]; then
    echo APPLIED
  elif dpkg --compare-versions "$MUTTER_UPSTREAM" ge 50.4; then
    if [[ -f "$MARKER" ]]; then echo FIXED_BY_UBUNTU; else echo FIXED; fi
  elif dpkg --compare-versions "$MUTTER_UPSTREAM" lt 50.0; then
    echo NOT_NEEDED
  elif [[ -f "$MARKER" ]]; then
    echo REPLACED_UNFIXED
  else
    echo NEEDS_PATCH
  fi
}

check_distro() {
  [[ "$ID" == ubuntu || "${FORCE:-}" == "1" ]] \
    || die "Ubuntu only (this: ${ID:-unknown}); set FORCE=1 to override"
  case "$VERSION_ID" in
    22.04|24.04|26.04) ;;
    *) log "warning: untested Ubuntu release '$VERSION_ID' (tested: 22.04, 24.04, 26.04)" ;;
  esac
}

check_build_env() {
  check_distro
  need_tools sudo apt-get patch dpkg-buildpackage dpkg-parsechangelog
  [[ -n "$CODENAME" ]] || die "cannot determine Ubuntu codename from $OS_RELEASE_FILE"
  [[ -n "$ABI_PKG" ]] || die "cannot find installed libmutter-*-0 package"
}

require_patch() {
  local state
  state=$(fix_state)
  case "$state" in
    NEEDS_PATCH) return 0 ;;
    APPLIED)
      log "already patched (mutter $MUTTER_VER); nothing to do"
      return 1
      ;;
    NOT_NEEDED)
      log "mutter $MUTTER_VER is older than 50.0; the ForwardKeyEvent bug is not present, no patch needed"
      return 1
      ;;
    FIXED|FIXED_BY_UBUNTU)
      log "mutter $MUTTER_VER already contains the upstream fix (>= 50.4); no patch needed"
      return 1
      ;;
    NO_MUTTER)
      die "mutter-common is not installed"
      ;;
    REPLACED_UNFIXED)
      log "warning: a previous +gotv patch was replaced by mutter $MUTTER_VER, which is still < 50.4; rebuilding"
      return 0
      ;;
    *)
      die "unknown mutter state: $state"
      ;;
  esac
}

source_state() {
  apt-get indextargets --format='$(type)' 2>/dev/null | grep -qx src
}

enable_deb_src() {
  source_state && return 0
  local f=/etc/apt/sources.list.d/$NAME-deb-src.sources
  sudo tee "$f" >/dev/null <<EOF
Types: deb-src
URIs: http://archive.ubuntu.com/ubuntu
Suites: $CODENAME $CODENAME-updates $CODENAME-security
Components: main universe restricted multiverse
EOF
  sudo apt-get update
  log "temporary deb-src enabled ($f)"
}

disable_deb_src() {
  local f=/etc/apt/sources.list.d/$NAME-deb-src.sources
  [[ -f "$f" ]] || return 0
  sudo rm -f "$f"
  sudo apt-get update
  log "temporary deb-src removed"
}

fetch_source() {
  mkdir -p "$WORK_DIR"
  cd "$WORK_DIR"
  if [[ -z "$(src_dir)" ]]; then
    apt-get source "$PKG" || die "apt-get source failed (deb-src enabled? network?)"
  fi
  [[ -d "$(src_dir)" ]] || die "no extracted source in $WORK_DIR"
}

src_dir() {
  find "$WORK_DIR" -maxdepth 1 -type d -name 'mutter-*' | grep -v '.dsc' | head -1
}

apply_patch() {
  local dir; dir=$(src_dir)
  printf '%s\n' "$PATCH_FILE" > "$WORK_DIR/570de65.patch"
  if grep -q 'CLUTTER_EVENT_FLAG_INPUT_METHOD' "$dir/clutter/clutter/clutter-event.c"; then
    log "patch already applied, continuing"
    return 0
  fi
  if ! dpkg --compare-versions "$MUTTER_UPSTREAM" ge 50.0; then
    die "refusing to apply the 50.x patch to mutter $MUTTER_VER (not affected); upstream did not backport it to this branch"
  fi
  patch -d "$dir" -p1 -N --dry-run < "$WORK_DIR/570de65.patch" >/dev/null \
    || die "patch does not apply to source in $dir (see $WORK_DIR/570de65.patch)"
  patch -d "$dir" -p1 < "$WORK_DIR/570de65.patch" >/dev/null
  grep -q 'CLUTTER_EVENT_FLAG_INPUT_METHOD' "$dir/clutter/clutter/clutter-event.c" \
    || die "patch apply verification failed"
}

bump_version() {
  local dir; dir=$(src_dir)
  local base
  base=$(dpkg-parsechangelog -l "$dir/debian/changelog" -S Version)
  printf 'mutter (%s+gotv1) unstable; urgency=medium\n\n  * backport 570de65: forward IME key events without a source device\n\n -- gotiengviet <dev@gotiengviet.local>  %s\n\n' \
    "$base" "$(date -R)" > "$WORK_DIR/_entry"
  cat "$dir/debian/changelog" >> "$WORK_DIR/_entry"
  mv "$WORK_DIR/_entry" "$dir/debian/changelog"
}

patch_packages() {
  PATCH_PKGS=("libmutter-${ABI_SERIES}-0" "mutter-common")
  package_installed "mutter-common-bin" && PATCH_PKGS+=("mutter-common-bin")
  package_installed "gir1.2-mutter-${ABI_SERIES}" && PATCH_PKGS+=("gir1.2-mutter-${ABI_SERIES}")
}

save_originals() {
  [[ -d "$ORIG_DEBS" ]] && return 0
  mkdir -p "$ORIG_DEBS"
  cd "$ORIG_DEBS"
  local p v
  for p in "${PATCH_PKGS[@]}"; do
    v=$(dpkg-query -W -f='${Version}' "$p")
    apt-get download "$p=$v" \
      || log "could not save original $p=$v (archive no longer serves it; restore will use current archive version)"
  done
}

build() {
  check_build_env
  require_patch || exit 0
  patch_packages
  enable_deb_src
  sudo apt-get build-dep -y "$PKG"
  fetch_source
  apply_patch
  bump_version
  local dir; dir=$(src_dir)
  ( cd "$dir" && dpkg-buildpackage -us -uc -b )
  ls "$WORK_DIR"/*.deb | grep -q . || die "build produced no .deb"
  log "built: $(ls "$WORK_DIR"/*.deb)"
}

install() {
  check_build_env
  require_patch || exit 0
  [[ $(ls "$WORK_DIR"/*.deb 2>/dev/null | grep -c .) -gt 0 ]] || build
  patch_packages
  save_originals
  local debs=()
  local p
  for p in "${PATCH_PKGS[@]}"; do
    local f
    f=$(ls "$WORK_DIR/${p}_"*.deb 2>/dev/null | head -1 || true)
    [[ -n "$f" ]] || die "missing built package for $p"
    debs+=("$f")
  done
  sudo dpkg -i "${debs[@]}"
  sudo mkdir -p "$(dirname "$MARKER")"
  printf '%s\n%s\n' "$(dpkg-query -W -f='${Version}' mutter-common)" "$(date -R)" \
    | sudo tee "$MARKER" >/dev/null
  log "patched mutter installed ($(dpkg-query -W -f='${Version}' mutter-common))"
  log "LOG OUT and log back in to apply the compositor change."
  log "restore anytime with: $0 restore"
}

restore() {
  check_build_env
  patch_packages
  if [[ -d "$ORIG_DEBS" ]] && ls "$ORIG_DEBS"/*.deb >/dev/null 2>&1; then
    sudo dpkg -i "$ORIG_DEBS"/*.deb
    log "restored saved originals from $ORIG_DEBS"
  else
    sudo apt-get install --reinstall --allow-downgrades -y "${PATCH_PKGS[@]}"
    log "restored ${PATCH_PKGS[*]} from archive"
  fi
  sudo rm -f "$MARKER"
  log "original mutter restored — LOG OUT and log back in."
}

status() {
  local state
  state=$(fix_state)
  printf 'OS: %s\n' "$PRETTY_NAME"
  printf 'mutter: %s\n' "${MUTTER_VER:-not installed}"
  printf 'ABI package: %s\n' "${ABI_PKG:-unknown}"
  printf 'state: %s\n' "$state"
  case "$state" in
    NOT_NEEDED)
      echo "backport: NOT_NEEDED (mutter < 50; forwarded key events carry a real device)"
      ;;
    NEEDS_PATCH)
      echo "backport: NEEDS_PATCH (mutter 50.0–50.3 drops ForwardKeyEvent)"
      echo "next: sudo $0 install"
      ;;
    APPLIED)
      echo "backport: APPLIED (+gotv build)"
      ;;
    FIXED|FIXED_BY_UBUNTU)
      echo "backport: NOT_NEEDED (mutter >= 50.4 already fixed)"
      ;;
    REPLACED_UNFIXED)
      echo "backport: NEEDS_PATCH (previous +gotv build was replaced by an older official version)"
      ;;
    NO_MUTTER)
      echo "backport: UNKNOWN (mutter-common not installed)"
      ;;
  esac
}

usage() {
  sed -n '2,22p' "$0" | sed 's/^# \?//'
}

trap 'disable_deb_src' EXIT

case "${1:-}" in
  build)   build ;;
  install) install ;;
  restore) restore ;;
  status|doctor) status ;;
  *) usage; exit 1 ;;
esac
