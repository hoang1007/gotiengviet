# GoTiengViet

Một bản fork của [ibus-bamboo](https://github.com/BambooEngine/ibus-bamboo), giúp việc gõ tiếng Việt trên Linux thuận tiện hơn.

Gõ tiếng Việt trên Linux vốn gặp nhiều hạn chế. Chế độ pre-edit với chữ gạch chân dễ làm mất từ khi chuyển cửa sổ làm việc. Trong khi các chế độ khác lại hay phát sinh lỗi lặp từ, sai dấu hoặc không xóa được chữ ở một số ứng dụng, buộc người dùng phải thay đổi chế độ gõ thủ công cho từng app.

GoTiengViet được phát triển để khắc phục các lỗi này, mang lại trải nghiệm gõ tiếng Việt liền mạch và quen thuộc tương tự trên Windows.

## Hướng dẫn cài đặt

### Cài nhanh

```sh
curl -fsSL https://github.com/hoang1007/gotiengviet/releases/latest/download/install.sh | bash
```

### Cài từ mã nguồn

Cài các gói cần thiết:

```sh
sudo apt install git golang-go build-essential dpkg-dev patch pkg-config ibus \
  libibus-1.0-dev libgtk-3-dev libx11-dev libxtst-dev
```

Tải mã nguồn và cài bộ gõ:

```sh
git clone https://github.com/hoang1007/gotiengviet.git
cd gotiengviet
sudo make install PREFIX=/usr
ibus restart
```

### Sau khi cài

Mở **Cài đặt → Bàn phím → Nguồn nhập**, thêm **Vietnamese (Bamboo)** và chọn bộ gõ đó. Bản fork mặc định dùng chế độ 3 (BackspaceForwardingIM); nếu đã có cấu hình Bamboo cũ, bạn có thể cần chọn lại chế độ này trong cài đặt bộ gõ.

Lệnh cài đặt dùng chung đường dẫn với ibus-bamboo thông thường và có thể ghi đè bản đã cài; hai bản không chạy song song như hai bộ gõ riêng.

### Gỡ cài đặt

Nếu cài bằng gói dựng sẵn:

```sh
sudo apt remove ibus-bamboo
```

Nếu cài từ mã nguồn, chạy trong thư mục mã nguồn:

```sh
sudo make uninstall PREFIX=/usr
```

Lệnh này chỉ gỡ bộ gõ, không khôi phục các gói Mutter nếu bạn đã cài bản vá.

## Giấy phép

GPL-3.0, cùng giấy phép với ibus-bamboo.
