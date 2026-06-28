# dmonitor-improved 仕様書

## 目的

JARL D-STAR委員会が配布しているdmonitor V02.00を、Raspberry Pi OS bookworm 32bit専用のままではなく、AMD64の通常PC上で扱えるように外部改善する。

このリポジトリではdmonitor本体を再実装しない。公式armhfバイナリを取得し、AMD64 PC上で`qemu-arm`を使って通常プロセスとして起動する実行基盤と、古いPerl CGIベースのWeb UIを置き換える管理UIを作る。

第一目標は、このPCに接続済みのIcom ID-50を`/dev/dstar`または`/dev/ttyACM0`経由で使い、Web UIからレピーターモニターの開始、停止、スキャン、状態確認を行えること。

## 前提と参考資料

- 公式説明書: `http://app.d-star.info/doc/dmonitor/dmonitor_02.00.pdf`
- 公式aptリポジトリ: `http://app.d-star.info/debian/bookworm/`
- apt source: `deb http://app.d-star.info/debian/bookworm/ /`
- key: `http://app.d-star.info/debian/bookworm/jarl-pkg.key`
- key fingerprint: `6C3F EFE8 3D17 4831 AF47 7667 0AE8 16B1 B609 3B5A`
- 対象公式パッケージ: `dmonitor_02.00_armhf.deb`
- パッケージSHA256: `ebb8085186214a337943129ed6ec3d8e6e29a1856a50add6e88691568d2c54ea`
- 公式パッケージのArchitectureは`armhf`で、主要バイナリはARM 32bit ELF。

dmonitorはD-STAR委員会の著作物であり、説明書でも無断再配布が禁止されている。そのため、このリポジトリにはdeb、展開済みrootfs、公式バイナリ、公式HTML/CGI資産をコミットしない。インストール時に利用者の環境で公式配布元から取得する。

Dockerは今回の仕様には含めない。QEMU VMも採用しない。AMD64ネイティブ環境で、`qemu-arm`経由により`rpt_conn`、`dmonitor`、`repeater_mon`などをワンライナー的に通常プロセスとして起動する。

## 現状の公式dmonitor構成

公式debには、主に次の構成が含まれる。

- armhf ELFバイナリ
  - `/usr/bin/dmonitor`
  - `/usr/bin/rpt_conn`
  - `/usr/bin/repeater_mon`
  - `/usr/bin/repeater_mon_light`
  - `/usr/bin/repeater_scan`
  - `/usr/bin/ci_v`
- shellスクリプト
  - `/usr/bin/auto_repmon`
  - `/usr/bin/auto_repmon_light`
  - `/usr/bin/kill_dmonitor`
  - `/usr/bin/rig_port_check`
- systemd unit
  - `auto_repmon.service`
  - `auto_repmon_light.service`
  - `rpt_conn.service`
  - `rfcomm.service`
  - `dstar_ntpdate.service`
- Perl CGIと旧Web UI
  - `/var/www/cgi-bin/*`
  - `/var/www/html/*`
- 設定ファイル
  - `/var/www/dmonitor.conf`
  - `/var/www/buff_hold.txt`
  - `/var/www/rpt_mast.txt`

公式実装はRaspberry Pi OS bookworm 32bit、systemd、lighttpd、Perl CGI、wiringPi、GPIO、`/proc/cpuinfo`のRaspberry Pi判定に強く依存している。このプロジェクトでは、これらのうちdmonitorバイナリ実行に必要な部分だけを互換レイヤで残し、運用制御とWeb UIは新規実装に置き換える。

## 実行方式

AMD64上にarmhf用の最小rootfsを作り、公式debと必要なarmhf依存ライブラリを展開する。各バイナリは次のような形で起動する。

```sh
qemu-arm -L ./runtime/rootfs ./runtime/rootfs/usr/bin/rpt_conn
qemu-arm -L ./runtime/rootfs ./runtime/rootfs/usr/bin/repeater_mon
qemu-arm -L ./runtime/rootfs ./runtime/rootfs/usr/bin/dmonitor '<CALLSIGN>' <IP_ADDRESS> <PORT> <AREA_CALLSIGN> [ZONE_CALLSIGN]
```

実装時は上記を直接ユーザーに打たせるのではなく、Goのプロセスマネージャが実行ラッパーとして管理する。

### rootfs作成

installerは次を行う。

1. JARL aptリポジトリの署名鍵を取得し、fingerprintを検証する。
2. `dmonitor_02.00_armhf.deb`を取得し、SHA256を検証する。
3. armhf rootfsにdebを展開する。
4. `libc6`、`libssl3`、`libusb-0.1-4`など、実行に必要なarmhf依存を展開する。
5. `/var/www/dmonitor.conf`、`/var/www/buff_hold.txt`、`/var/www/rpt_mast.txt`など、公式バイナリが参照するファイル配置をrootfs内に用意する。
6. rootfs外の実デバイス`/dev/dstar`をrootfs内から参照できるようにする。

NixOSを含む非Debianホストでも動かすため、ホストのaptには依存しない方針にする。deb展開には`bsdtar`、`dpkg-deb`相当、または独自のar/tar処理を使う。

### systemd置き換え

systemd unitは使わない。Go backendが次の責務を持つ。

- `rpt_conn`の起動、停止、再起動、状態監視
- `repeater_mon`または`repeater_mon_light`の5秒周期更新プロセス管理
- `dmonitor`接続プロセスの起動、SIGINT/SIGTERM停止、PIDファイル掃除
- `repeater_scan`の起動と停止
- `SIGUSR1`/`SIGUSR2`によるdmonitorバッファ増減
- stdout/stderrと関連ログファイルの集約
- 公式CGIが行っていた一時HTMLファイル初期化

旧`auto_repmon`のループはGo側で再実装する。具体的には5秒ごとに`repeater_mon`を実行し、存在しない場合は`connected_table.html`、`repeater_active.html`、`repeater_mon.html`、`repeater_scan.html`、`error_msg.html`、`short_msg.html`の互換ファイルを初期化する。

### Raspberry Pi依存の偽装

公式バイナリやwiringPiがRaspberry Pi環境を前提にして失敗する箇所は、armhf用`LD_PRELOAD`ライブラリで吸収する。

最低限フックするもの:

- `/proc/cpuinfo`を開く`fopen`、`open`、`openat`
- GPIOや`/dev/mem`/`/dev/gpiomem`由来の`mmap`

`/proc/cpuinfo`にはRaspberry Pi相当の内容を返す。GPIOメモリマップ要求にはダミー領域を返し、LCD/GPIO未使用時にバイナリが即死しないようにする。

この互換ライブラリはarmhfバイナリとしてビルドし、`qemu-arm`で起動する各対象プロセスに`LD_PRELOAD`で注入する。

## 無線機とデバイス

第一対応デバイスはID-50。現在の実機では次の状態を前提にする。

- `/dev/ttyACM0`でアクセス可能
- `/dev/dstar -> ttyACM0`
- USB vendor/product: `0c26:0046`
- `MODE="0666"`相当でアプリケーションからアクセス可能

udevルールは次を含める。

```udev
SUBSYSTEM=="tty", ATTRS{idVendor}=="0403", ATTRS{idProduct}=="6001", SYMLINK+="dstar", MODE="0666"
SUBSYSTEM=="tty", ATTRS{idVendor}=="0c26", ATTRS{idProduct}=="0036", SYMLINK+="dstar", MODE="0666"
SUBSYSTEM=="tty", ATTRS{idVendor}=="0c26", ATTRS{idProduct}=="003a", SYMLINK+="dstar", MODE="0666"
SUBSYSTEM=="tty", ATTRS{idVendor}=="0c26", ATTRS{idProduct}=="0046", SYMLINK+="dstar", MODE="0666"
SUBSYSTEM=="tty", ATTRS{idVendor}=="0c26", ATTRS{idProduct}=="004c", SYMLINK+="dstar", MODE="0666"
```

Go backendは起動時に`/dev/dstar`と`/dev/ttyACM0`を検出し、Web UIにデバイス状態を表示する。`/dev/dstar`が存在しない場合は、udevルールの導入方法をUIとログに出す。

同時に複数の無線機や同種USB変換ケーブルを使う構成は初期対応外にする。

## dmonitor設定互換

公式Web UIが使う`/var/www/dmonitor.conf`形式を維持する。

```text
ICOM
NONE
JL1IZA A
NO_SKIP
```

意味:

- 1行目: 接続リグ。初期対応は`ICOM`。
- 2行目: LCDタイプ。初期対応は`NONE`。
- 3行目: 接続コールサイン。8文字固定幅に整形する。
- 4行目: GPS自動送信。`SKIP`または`NO_SKIP`。

Web UIとAPIはこの形式を読み書きする。保存時は公式CGIと同様にコールサインを大文字化し、前後空白を調整し、8文字固定幅にする。

## 公式バイナリ仕様

### dmonitor

PDF記載のコマンド仕様:

```sh
/usr/bin/dmonitor connect_callsign ip_address port area_callsign [ZONE_callsign]
```

- `connect_callsign`: 接続時のコールサイン
- `ip_address`: 接続先レピーターのグローバルIPアドレス
- `port`: 接続先待ち受けポート。通常`51000`
- `area_callsign`: 接続先レピーターのエリアコールサイン
- `zone_callsign`: 接続先レピーターのゾーンコールサイン

`dmonitor`はデーモンとして起動する想定のため、起動直後に制御が戻る場合がある。Go側はプロセス終了だけでなく、PIDファイル、ログ、互換HTMLファイルも見て状態判定する。

停止時は公式CGIと同様にSIGINT相当を送り、最大15秒待つ。残ったPIDファイルはGo側で掃除する。

### rpt_conn

`rpt_conn`は無線機からの操作でレピーターに接続する常駐プロセス。Web UIを開いていない状態でも動作できるようにする。

無線機側URに設定する文字列:

- 接続: エリアCQ
- 切断: `DISCON`または`UNLINK`
- スキャン: `SCAN`
- 状態表示: `STATUS`
- 再起動: `REBOOT`
- シャットダウン: `SHUTDOWN`

初期版では`REBOOT`と`SHUTDOWN`はホストOSに対して実行しない。Web UIとログに「未対応」と出し、危険なホスト操作を避ける。

`rpt_conn`起動時は`dmonitor`を停止し、必要に応じて`repeater_mon_light`相当の更新ループへ切り替える。Web UI操作時は`rpt_conn`と`dmonitor`の接続先競合をGo側が調停する。

### repeater_mon / repeater_mon_light

`repeater_mon`はレピーター一覧や接続状態のHTMLファイルを生成する。公式`auto_repmon`では5秒ごとに実行されているため、Go側でも5秒周期を標準にする。

`repeater_mon_light`はWebを使わず`rpt_conn`中心で使う場合の軽量更新として扱う。

### repeater_scan

`repeater_scan`は一覧に表示されているレピーターを順にスキャンする。スキャン開始時は`dmonitor`を停止し、既存の`repeater_scan`が残っていれば停止する。

## Web UI / API

古いPerl CGI、lighttpd、frameset UIは使わない。Go backendとReact/Vite UIに置き換える。

### Go backend

最低限のAPI:

- `GET /api/status`
  - 実行中プロセス、現在接続、デバイス状態、設定概要、直近エラーを返す。
- `GET /api/config`
  - `dmonitor.conf`互換設定を返す。
- `PUT /api/config`
  - `dmonitor.conf`互換設定を保存する。
- `POST /api/runtime/start-rpt-conn`
  - `rpt_conn`を起動または再起動する。
- `POST /api/runtime/stop-rpt-conn`
  - `rpt_conn`を停止する。
- `POST /api/monitor/connect`
  - `dmonitor connect_callsign ip_address port area_callsign [zone_callsign]`を起動する。
- `POST /api/monitor/disconnect`
  - `dmonitor`を停止し、必要なら`rpt_conn`へ戻す。
- `POST /api/repeater/scan/start`
  - `repeater_scan`を起動する。
- `POST /api/repeater/scan/stop`
  - `repeater_scan`を停止する。
- `POST /api/repeater/update`
  - `http://log.d-star.info/usr/rpt_mast.txt`を取得して`/var/www/rpt_mast.txt`を更新する。
- `POST /api/buffer/increase`
  - `dmonitor`へ`SIGUSR1`を送る。
- `POST /api/buffer/decrease`
  - `dmonitor`へ`SIGUSR2`を送る。
- `GET /api/logs`
  - `dmonitor.log`、`rpt_conn.log`、stderr/stdout、更新ログを返す。

APIはlocalhost運用を標準にする。LAN公開は明示設定がある場合のみ許可する。

### React UI

初期UIは1画面中心の操作パネルにする。

表示するもの:

- デバイス状態
- `rpt_conn`状態
- `dmonitor`接続状態
- 現在の接続先
- レピーター一覧
- スキャン状態
- ログ
- 設定フォーム

操作:

- 設定保存
- レピーターリスト更新
- レピーター接続
- 切断
- スキャン開始/停止
- `rpt_conn`再起動
- バッファ拡張/縮小

公式HTMLが生成する`repeater_mon.html`などは初期版では互換ファイルとして読み取り、React側で最低限パースして一覧表示する。後続で構造化データ化できるよう、HTMLパース部分はGo backend内に閉じ込める。

## ログと状態管理

Go backendは次の状態を保持する。

- rootfsの場所
- qemu-armの場所
- `LD_PRELOAD`ライブラリの場所
- `/dev/dstar`の実体
- 各プロセスのPID、開始時刻、終了コード
- 現在の接続先レピーター
- 最後のエラー

ログは次を集約する。

- `dmonitor` stdout/stderr
- `rpt_conn` stdout/stderr
- `repeater_mon` stdout/stderr
- rootfs内の`/var/log/dmonitor.log`
- rootfs内の`/var/log/rpt_conn.log`
- rootfs内の`/var/tmp/update.log`
- rootfs内の`/var/tmp/error_msg.html`
- rootfs内の`/var/tmp/short_msg.html`

## 成功条件

初期実装の成功条件:

1. AMD64 PC上でQEMU VMなしに`qemu-arm`経由で公式armhfバイナリを起動できる。
2. ID-50が`/dev/dstar`経由で認識される。
3. Go backendから`rpt_conn`を起動し、即時クラッシュしない。
4. Web UIから設定を保存できる。
5. Web UIからレピーター一覧を更新または表示できる。
6. Web UIから`dmonitor`接続を開始できる。
7. Web UIから切断し、`rpt_conn`待受へ戻せる。
8. Perl CGI、lighttpd、systemdを使わずに上記を満たす。

## テスト計画

### 静的確認

- 公式PDFのコマンド仕様と設定ファイル仕様に沿っていることを確認する。
- 公式debのSHA256を検証する。
- 展開した主要バイナリがARM 32bit ELFであることを確認する。
- rootfs内に必要な設定ファイルと一時ディレクトリが作成されることを確認する。

### 単体テスト

- `dmonitor.conf`読み書き
- コールサインの大文字化、8文字固定幅化
- プロセス状態遷移
- PIDファイル掃除
- HTML互換ファイルの初期化
- udevデバイス検出
- APIの入力バリデーション

### 結合テスト

- `qemu-arm`で`repeater_mon`を実行し、互換HTMLが生成されること。
- `LD_PRELOAD`有効状態で`rpt_conn`がRaspberry Pi/GPIO依存で即死しないこと。
- ID-50接続状態で`rpt_conn`を起動できること。
- Web UIから`dmonitor`接続、切断、スキャン開始/停止を操作できること。
- `SIGUSR1`/`SIGUSR2`でバッファ増減操作ができること。

### 手動確認

- `/dev/dstar -> /dev/ttyACM0`を確認する。
- `udevadm info -q property -n /dev/ttyACM0`で`ID_VENDOR_ID=0c26`、`ID_MODEL_ID=0046`を確認する。
- ID-50側の簡易メッセージに`rpt_conn`または`dmonitor`由来の状態が出ることを確認する。
- `STATUS`、`SCAN`、`DISCON`/`UNLINK`相当の操作を確認する。

## 実装順序

1. 公式deb取得とarmhf rootfs展開を行うinstallerを作る。
2. `qemu-arm`実行ラッパーを作る。
3. `LD_PRELOAD`互換ライブラリをarmhf向けに作る。
4. Goプロセスマネージャで`repeater_mon`、`rpt_conn`、`dmonitor`を起動できるようにする。
5. `dmonitor.conf`互換設定の読み書きを実装する。
6. Go APIを実装する。
7. React UIを実装する。
8. ID-50実機で接続、切断、スキャン、ログ表示を確認する。

## 非対応事項

- Docker構成
- QEMU VM構成
- dmonitor公式バイナリの再配布
- rootfs成果物のコミット
- 複数無線機の同時利用
- Bluetooth接続
- DVAP、DVMEGA、NODE Adapter、CI-Vの実機保証
- ホストOSの`REBOOT`/`SHUTDOWN`操作

## 将来検討

- DVAP、DVMEGA、NODE Adapterの実機対応
- IC-705 CI-V対応
- Bluetooth対応
- 公式HTML出力の構造化パーサ強化
- LAN公開時の認証
- Nix package化
