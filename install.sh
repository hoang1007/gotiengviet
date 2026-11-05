#!/usr/bin/env bash
# Install the prebuilt GoTiengViet release on Ubuntu 22.04, 24.04 or 26.04 (amd64).
#
# Mutter 570de65 is only needed on 26.04 (mutter 50.0–50.3). Ubuntu 22.04
# (mutter 42.x) and 24.04 (mutter 46.x) already forward IME key events with a
# real keyboard device, so they install the engine only.
set -euo pipefail

release=release/v0.8.5-gotv1
base="https://github.com/hoang1007/gotiengviet/releases/download/$release"
mutter_base=50.1-0ubuntu2.5
mutter_version="${mutter_base}+gotv1"
mutter_lib="libmutter-18-0_${mutter_version}_amd64.deb"
mutter_common="mutter-common_${mutter_version}_all.deb"
mutter_gir="gir1.2-mutter-18_${mutter_version}_amd64.deb"

die() { printf 'Lỗi: %s\n' "$*" >&2; exit 1; }

[[ -r /etc/os-release ]] || die 'Không xác định được bản phân phối.'
# shellcheck source=/dev/null
. /etc/os-release
[[ ${ID:-} == ubuntu ]] || die 'Bản cài này chỉ hỗ trợ Ubuntu.'
case "${VERSION_ID:-}" in
  22.04|24.04|26.04) ;;
  *) die "Bản cài này hỗ trợ Ubuntu 22.04, 24.04 và 26.04 (bạn đang dùng ${VERSION_ID:-không rõ})." ;;
esac

arch=$(dpkg --print-architecture)
[[ "$arch" == amd64 ]] || die "Bản cài này chỉ hỗ trợ máy amd64 (bạn đang dùng $arch)."
for cmd in curl sha256sum dpkg dpkg-deb sudo apt-get; do
  command -v "$cmd" >/dev/null || die "Thiếu lệnh $cmd."
done

engine_version="0.8.5+gotv1-1"
# 26.04 giữ tên gói cũ để không phá bản phát hành hiện tại. Các bản LTS cũ
# dùng hậu tố ~<release> để tránh cài nhầm binary build trên 26.04.
if [[ "$VERSION_ID" != 26.04 ]]; then
  engine_version+="~${VERSION_ID}"
fi
# GitHub lưu asset với tên thay '~' bằng '.', nên tên file công bố dùng '.';
# version Debian bên trong gói (engine_version) vẫn giữ '~'.
file_version="${engine_version//\~/.}"
engine="ibus-bamboo_${file_version}_${arch}.deb"

work=$(mktemp -d "${TMPDIR:-/tmp}/gotv-install.XXXXXXXX")
trap 'rm -rf "$work"' EXIT
curl -fsSL "$base/SHA256SUMS" -o "$work/SHA256SUMS" \
  || die 'Không tải được thông tin bản phát hành.'
grep -Fq "  $engine" "$work/SHA256SUMS" \
  || die "Bản phát hành $release chưa có gói cho Ubuntu $VERSION_ID ($engine)."

download() {
  local name=$1
  curl -fsSL "$base/$name" -o "$work/$name" || die "Không tải được $name."
  (cd "$work" && grep -F "  $name" SHA256SUMS | sha256sum --check --status) \
    || die "Kiểm tra mã băm thất bại: $name."
}

check_deb() {
  local file=$1 package=$2 version=$3 arch=$4
  [[ $(dpkg-deb -f "$work/$file" Package) == "$package" && \
     $(dpkg-deb -f "$work/$file" Version) == "$version" && \
     $(dpkg-deb -f "$work/$file" Architecture) == "$arch" ]] \
    || die "Thông tin gói không đúng: $file."
}

download "$engine"
check_deb "$engine" ibus-bamboo "$engine_version" "$arch"

patch_mutter=0
sync_mutter=0
packages=("$work/$engine")

if [[ "$VERSION_ID" == 26.04 ]]; then
  lib_version=$(dpkg-query -W -f='${Version}' libmutter-18-0 2>/dev/null || true)
  common_version=$(dpkg-query -W -f='${Version}' mutter-common 2>/dev/null || true)

  if [[ "$lib_version" == "$mutter_base" && "$common_version" == "$mutter_base" ]]; then
    patch_mutter=1
    sync_mutter=1
  elif [[ "$lib_version" == "$mutter_version" && "$common_version" == "$mutter_version" ]]; then
    : # already patched
  elif [[ ${GOTV_ASSUME_MUTTER_FIXED:-0} == 1 ]]; then
    printf 'Giả định Mutter %s đã có bản sửa (GOTV_ASSUME_MUTTER_FIXED=1).\n' "${lib_version:-?}"
  elif [[ -n "$lib_version" && "$lib_version" == "$common_version" ]] && \
       dpkg --compare-versions "$lib_version" ge 50.4; then
    printf 'Mutter %s đã có bản sửa upstream.\n' "$lib_version"
  else
    die "Chưa có bản vá được kiểm thử cho Mutter '${lib_version:-không tìm thấy}' / '${common_version:-không tìm thấy}'. Không thay đổi hệ thống."
  fi

  if (( patch_mutter )) || [[ "$lib_version" == "$mutter_version" ]]; then
    # Avoid silently removing developer/test packages with exact dependencies.
    for package in libmutter-18-dev libmutter-test-18 mutter-18-tests \
      libmutter-18-0-dbgsym libmutter-test-18-dbgsym mutter-18-tests-dbgsym; do
      status=$(dpkg-query -W -f='${db:Status-Status}' "$package" 2>/dev/null || true)
      [[ "$status" != installed ]] || die "Máy có gói $package; cần xử lý phụ thuộc Mutter thủ công."
    done
    gir_status=$(dpkg-query -W -f='${db:Status-Status}' gir1.2-mutter-18 2>/dev/null || true)
    if [[ "$gir_status" == installed ]]; then
      gir_version=$(dpkg-query -W -f='${Version}' gir1.2-mutter-18)
      if [[ "$gir_version" != "$mutter_version" ]]; then
        dpkg --compare-versions "$gir_version" lt "$mutter_version" \
          || die 'Không thể hạ phiên bản gir1.2-mutter-18.'
        sync_mutter=1
      fi
    fi

    if (( sync_mutter )); then
      [[ -r /dev/tty ]] || die 'Cần terminal để xác nhận cài bản vá Mutter.'
      printf 'Cần cài hoặc đồng bộ các gói Mutter đã vá (%s).\n' "$mutter_version"
      printf 'Tiếp tục? [y/N] '
      read -r answer </dev/tty
      [[ "$answer" == [yY] || "$answer" == [yY][eE][sS] ]] \
        || die 'Đã huỷ cài đặt; Mutter chưa được thay đổi.'
    fi

    if (( patch_mutter )); then
      download "$mutter_lib"
      download "$mutter_common"
      check_deb "$mutter_lib" libmutter-18-0 "$mutter_version" amd64
      check_deb "$mutter_common" mutter-common "$mutter_version" all
      packages+=("$work/$mutter_lib" "$work/$mutter_common")
    fi
    if [[ "$gir_status" == installed && "$gir_version" != "$mutter_version" ]]; then
      download "$mutter_gir"
      check_deb "$mutter_gir" gir1.2-mutter-18 "$mutter_version" amd64
      packages+=("$work/$mutter_gir")
    fi
  else
    gir_status=not-installed
  fi
else
  # 22.04/24.04: no mutter backport is needed, but refuse the known-bad range
  # if a user somehow has a newer mutter installed.
  lib_pkg=$(dpkg-query -W -f='${db:Status-Status} ${Package}\n' 'libmutter-*-0' 2>/dev/null \
            | awk '$1 == "installed" { print $2; exit }')
  lib_version=$(dpkg-query -W -f='${Version}' "${lib_pkg:-mutter-common}" 2>/dev/null || true)
  upstream=${lib_version#*:}
  upstream=${upstream%%-*}
  if [[ -n "$upstream" ]] && dpkg --compare-versions "$upstream" ge 50.0 && \
     dpkg --compare-versions "$upstream" lt 50.4; then
    die "Mutter $lib_version cần backport 570de65; chạy scripts/mutter-fix.sh trên Ubuntu $VERSION_ID trước."
  fi
  printf 'Mutter %s: không cần backport 570de65.\n' "${lib_version:-không tìm thấy}"
fi

apt-get -s install --no-remove "${packages[@]}" \
  || die 'APT không thể cài an toàn các gói này; chưa thay đổi hệ thống.'

sudo apt-get install -y --no-remove "${packages[@]}"
if command -v ibus >/dev/null; then
  ibus restart || printf 'Không khởi động lại được IBus; hãy đăng xuất và đăng nhập lại.\n'
fi
printf 'Đã cài GoTiengViet. Thêm Vietnamese (Bamboo) trong Cài đặt → Bàn phím → Nguồn nhập.\n'
if (( patch_mutter )); then
  printf 'Hãy đăng xuất và đăng nhập lại để bản vá Mutter có hiệu lực.\n'
fi
