# Phát hành GoTiengViet

Toàn bộ phiên bản nằm trong file [`VERSION`](VERSION). Không sửa version rải
rác trong script hay workflow; CI sẽ từ chối tag không khớp với file này.

## Định dạng

```
VERSION:  0.8.5+gotv2-1
Tag:      release/v0.8.5-gotv2
```

- `0.8.5` — phiên bản upstream ibus-bamboo.
- `+gotv2` — revision của fork GoTiengViet.
- `-1` — revision Debian (tăng khi đóng gói lại cùng một bản mã nguồn).

Tag bỏ `+gotv` thành `-gotv`, và bỏ `-<revision Debian>`:

```
release/v<upstream>-gotv<revision>
```

## Các bước phát hành

1. Sửa `VERSION`:

   ```sh
   printf '0.8.5+gotv3-1\n' > VERSION
   ```

2. Kiểm tra và commit:

   ```sh
   scripts/version.sh        # in ra và validate version
   make test                 # hoặc: go test -mod=vendor -count=1 -race ./...
   git add VERSION
   git commit -m 'build(release): bump GoTiengViet revision to gotv3'
   git push origin master
   ```

3. Tạo và đẩy tag:

   ```sh
   git tag release/v0.8.5-gotv3
   git push origin release/v0.8.5-gotv3
   ```

Tag `release/*` kích hoạt `.github/workflows/release.yml`, workflow sẽ:

- suy ra `0.8.5+gotv3-1` từ tag và đối chiếu với `VERSION`;
- build engine trên Ubuntu 22.04, 24.04, 26.04;
- build bản vá Mutter;
- công bố GitHub release kèm `install.sh` và `SHA256SUMS`.

Nếu tag và `VERSION` lệch nhau, job `build-engine` dừng ngay với thông báo
không khớp.

## Những chỗ tự động lấy version

| Thành phần | Nguồn |
| --- | --- |
| `make build`, `scripts/build` | `scripts/version.sh` |
| `scripts/build-gotv-deb.sh` | `scripts/version.sh` |
| `.github/workflows/release.yml` | tag, đối chiếu với `VERSION` |
| `install.sh` | chọn asset từ `SHA256SUMS` của bản phát hành |

`install.sh` không hardcode version: mặc định tải bản `latest`, và có thể ghim
bằng `GOTV_RELEASE=release/v0.8.5-gotv3`.

## Mutter

Bản vá Mutter dùng revision riêng `+gotv1` (`scripts/mutter-fix.sh`) và **không**
tăng theo revision của engine. Chỉ đổi khi bản thân bản vá backport thay đổi.
