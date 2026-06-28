# dmonitor-improved

JARL D-STAR委員会配布の dmonitor V02.00 armhf バイナリを再配布せず、AMD64 PC 上で `qemu-arm` 経由の通常プロセスとして扱うための実行基盤と管理 UI です。

## 構成

- `cmd/dmonitor-install`: 公式 apt リポジトリから `dmonitor_02.00_armhf.deb` を取得し、SHA256 を検証して `runtime/rootfs` に展開します。
- `cmd/dmonitor-improved`: localhost 専用の Go backend と API サーバーです。
- `internal/runtime`: `rpt_conn`、`dmonitor`、`repeater_mon`、`repeater_scan` の起動・停止・ログ集約を行います。
- `compat/dmonitor_compat.c`: armhf 向け `LD_PRELOAD` 互換レイヤです。`/proc/cpuinfo` の Raspberry Pi 偽装、GPIO メモリ要求のダミー化、`/var/www` などの rootfs リダイレクトを行います。
- `web`: React/Vite の管理 UI です。
- `udev/99-dmonitor.rules`: ID-50 を含む `/dev/dstar` 作成用 udev ルールです。

公式 deb、展開済み rootfs、公式 HTML/CGI 資産、公式バイナリはコミットしません。

## セットアップ

```sh
nix develop
make install-rootfs
make build-compat
npm --prefix web install
npm --prefix web run build
go run ./cmd/dmonitor-improved
```

`nix develop` で `go`、`npm`、`qemu-arm`、`gpg`、`file`、`curl`、`arm-linux-gnueabihf-gcc` など、実行と検証に必要なツールが入った devShell に入れます。

udev ルールを導入する場合:

```sh
sudo install -m 0644 udev/99-dmonitor.rules /etc/udev/rules.d/99-dmonitor.rules
sudo udevadm control --reload-rules
sudo udevadm trigger
```

## Docker

Docker イメージには公式 dmonitor の deb、展開済み rootfs、公式バイナリは含めません。初回起動時に公式配布元から取得し、`dmonitor-rootfs` volume に展開します。

```sh
docker compose up --build
```

明示的に rootfs を作成したい場合:

```sh
docker compose run --rm dmonitor install-rootfs
docker compose up
```

UI はホスト側の `http://localhost:8080` で開きます。ID-50 などの実デバイスを使う場合は、ホスト側で `/dev/dstar` を作成してから `docker-compose.yml` の `devices` 設定を有効化してください。

Make ターゲットも用意しています。

```sh
make docker-build
make docker-install-rootfs
make docker-up
```

## API

- `GET /api/status`
- `GET /api/config`
- `PUT /api/config`
- `POST /api/runtime/start-rpt-conn`
- `POST /api/runtime/stop-rpt-conn`
- `POST /api/monitor/connect`
- `POST /api/monitor/disconnect`
- `POST /api/repeater/scan/start`
- `POST /api/repeater/scan/stop`
- `POST /api/repeater/update`
- `POST /api/buffer/increase`
- `POST /api/buffer/decrease`
- `GET /api/logs`

backend は標準で `127.0.0.1:8080` にだけ bind します。LAN 公開は reverse proxy などで明示的に制御してください。
