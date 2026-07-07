# Cacti RRD REST API Bridge (Golang)

REST API modern, tangguh, dan berkinerja tinggi ditulis dalam Golang untuk membaca dan mengekspor data RRD Cacti (versi 1.2.30). API ini dirancang untuk diintegrasikan secara langsung dengan dashboard web kustom dan **Grafana** (menggunakan plugin JSON API atau Infinity).

---

## 🚀 Fitur Utama

- **Read-Only Guarantee**: API ini hanya menjalankan perintah `xport`, `graph`, dan `info`. Tidak ada modifikasi atau penulisan data ke database Cacti.
- **Strict Path Security**: Melakukan sanitasi parameter input untuk mencegah *Path Traversal* (e.g., penggunaan `..` atau path absolut di luar direktori RRD Cacti).
- **Background Metrics Cache**: Memindai direktori RRD dan memetakan data source (DS) di latar belakang secara periodik. Menghilangkan delay disk I/O saat memanggil `/api/v1/list_metrics` pada sistem skala besar dengan ribuan file RRD.
- **HMAC-SHA256 Signature Authentication**: Mendukung verifikasi query string menggunakan tanda tangan HMAC-SHA256 untuk membatasi akses tidak sah.
- **Interactive Web Dashboard**: Menyediakan dashboard bawaan (SPA modern dengan glassmorphism dark-mode) untuk memantau data, melakukan query rentang waktu, dan memvisualisasikan grafik SVG langsung dari browser.
- **Built-in Demo Mode**: Jika server tidak mendeteksi `rrdtool` di sistem lokal, ia secara otomatis berjalan dalam mode demo dengan data tiruan dan visualisasi SVG dinamis agar Anda bisa langsung mencobanya.

---

## 🌐 Enterprise Networking & Reliability Standards

API ini dioptimalkan dengan standar industri enterprise untuk memastikan keandalan, observability, dan keamanan:

1. **Production-Grade Connection Timeouts**: Mencegah kebocoran resource dan serangan Slowloris dengan mengonfigurasi batas waktu baca/tulis koneksi secara eksplisit (`ReadTimeout: 10s`, `WriteTimeout: 30s`, `IdleTimeout: 120s`, `ReadHeaderTimeout: 5s`).
2. **Graceful Shutdown**: Menangani sinyal operasi OS (`SIGINT`, `SIGTERM`) dengan waktu tenggang 15 detik agar koneksi aktif (e.g., query data besar oleh Grafana) dapat diselesaikan sebelum server berhenti.
3. **Structured JSON Logging**: Log server dicetak dalam format JSON standar industri untuk integrasi langsung ke sistem agregator log seperti **Datadog, Grafana Loki, Splunk, dan ELK stack**.
4. **Distributed Request Tracing**: Menyematkan `X-Request-ID` unik di setiap request untuk melacak rentetan aliran data di jaringan.
5. **Native TLS (HTTPS)**: Mendukung pengikatan langsung sertifikat SSL/TLS untuk lalu lintas data terenkripsi secara native di level Go.
6. **API Rate Limiting**: Membatasi scraping berlebih dengan algoritme *Token-Bucket* bawaan yang thread-safe.
7. **OS Process Concurrency Throttling**: Membatasi jumlah eksekusi proses biner `rrdtool` secara bersamaan melalui sistem semaphore untuk mencegah kehabisan kapasitas CPU (*resource starvation*).

---

## 🏗️ Arsitektur Kode (Clean Code & SOLID)

Arsitektur API ini diatur secara modular untuk memudahkan pemeliharaan dan pengujian:
```text
cacti-rrd-api/
├── main.go              # Entry point & Setup HTTP Server (Graceful Shutdown & Timeouts)
├── internal/
│   ├── api/             # HTTP Route, Handlers, Middleware (Auth, CORS, JSON Logger, Rate Limit)
│   ├── config/          # Konfigurasi loader (Env, Flags, JSON) - Tanpa Global State
│   ├── querysign/       # Implementasi validasi tanda tangan HMAC-SHA256
│   └── rrd/             # Abstraksi RRDClient (CLI client & Mock client) dan Metrics Cache
├── web/
│   └── index.html       # Single Page Application Dashboard (Embedded)
├── go.mod
└── go.sum
```

---

## 🛠️ Cara Menjalankan & Membangun

### Prasyarat
- Go versi 1.22 atau lebih baru.
- `rrdtool` terinstal di sistem target (misal: `sudo apt install rrdtool`).

### Membangun Project
```bash
go build -o cacti-rrd-api-server main.go
```

### Menjalankan Server secara Manual
```bash
# Menjalankan dengan parameter kustom
./cacti-rrd-api-server -listen :9191 -rrd-dir /var/www/html/cacti/rra -secret "your_secret_here"
```

---

## ⚙️ Parameter Konfigurasi (Production Options)

Konfigurasi dapat diatur menggunakan **JSON File**, **Environment Variables (ENV)**, atau **CLI Flags**.

| Fitur | Environment Variable | CLI Flag | Default | Deskripsi |
| :--- | :--- | :--- | :--- | :--- |
| **Address** | `RRD_LISTEN_ADDRESS` | `-listen` | `0.0.0.0:9191` | Alamat listen server API |
| **RRD Dir** | `RRD_DIR` | `-rrd-dir` | `/var/www/html/cacti/rra` | Path folder file `.rrd` |
| **Command** | `RRDTOOL_COMMAND` | `-rrdtool-bin` | `rrdtool` | Path menuju file binary rrdtool |
| **Secret Key** | `RRD_SIGNED_QUERY_SECRET`| `-secret` | `""` | Kunci HMAC untuk signed URL |
| **Basic Auth User**| `RRD_BASIC_AUTH_USER` | `-auth-user` | `""` | Username untuk Basic Authentication |
| **Basic Auth Pass**| `RRD_BASIC_AUTH_PASS` | `-auth-pass` | `""` | Password untuk Basic Authentication |
| **Refresh Cache** | `RRD_REFRESH_INTERVAL` | `-` | `5m` | Durasi reload daftar file RRD |
| **Demo Mode** | `RRD_DEMO_MODE` | `-demo` | `false` | Menjalankan API dengan simulasi data |
| **TLS Cert** | `RRD_TLS_CERT_FILE` | `-tls-cert` | `""` | Path ke berkas sertifikat `.crt` |
| **TLS Key** | `RRD_TLS_KEY_FILE` | `-tls-key` | `""` | Path ke berkas private key `.key` |
| **Rate Limit RPS** | `RRD_RATE_LIMIT_RPS` | `-rate-rps` | `20.0` | Limit request per detik (0 = disable) |
| **Rate Limit Burst**| `RRD_RATE_LIMIT_BURST`| `-rate-burst`| `50` | Maksimum burst request rate |
| **Max CLI Conns** | `RRD_MAX_CONCURRENT_RRDTOOL`| `-max-conns`| `10` | Limit concurrent rrdtool process |

---

## 🚀 Setup Produksi (Production Deployment)

### 1. Menjalankan sebagai Systemd Service
Untuk memastikan API berjalan terus di background secara tangguh dan otomatis menyala kembali jika crash atau server di-reboot.

Buat file unit service baru di `/etc/systemd/system/cacti-rrd-api.service`:

```ini
[Unit]
Description=Cacti RRD REST API Bridge
After=network.target

[Service]
Type=simple
User=cacti # Pastikan user ini memiliki akses baca (Read-only) ke folder RRD
WorkingDirectory=/home/homenet/dev/cacti
LimitNOFILE=65535

# Konfigurasi via Environment Variables
Environment=RRD_LISTEN_ADDRESS=127.0.0.1:9191
Environment=RRD_DIR=/var/www/html/cacti/rra
Environment=RRDTOOL_COMMAND=/usr/bin/rrdtool
Environment=RRD_SIGNED_QUERY_SECRET=5478b004f3f97a56a32b95a2c559fcf6bcfcd0e1712692839f32b3b4dea01ff9
Environment=RRD_RATE_LIMIT_RPS=30
Environment=RRD_RATE_LIMIT_BURST=60
Environment=RRD_MAX_CONCURRENT_RRDTOOL=12

ExecStart=/home/homenet/dev/cacti/cacti-rrd-api-server
Restart=always
RestartSec=5s

# Pengamanan Tambahan (Sandboxing)
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/tmp
ReadOnlyPaths=/var/www/html/cacti/rra

[Install]
WantedBy=multi-user.target
```

Nyalakan dan jalankan service:
```bash
sudo systemctl daemon-reload
sudo systemctl enable cacti-rrd-api
sudo systemctl start cacti-rrd-api
sudo systemctl status cacti-rrd-api
```

---

### 2. Konfigurasi Nginx Reverse Proxy (HTTPS Termination)
Untuk mengekspos API secara aman melalui domain publik menggunakan HTTPS dengan sertifikat Let's Encrypt / SSL enterprise.

```nginx
# Batasan rate limit opsional di level Nginx
limit_req_zone $binary_remote_addr zone=cacti_api_limit:10m rate=30r/s;

server {
    listen 80;
    server_name cacti-api.company.local;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name cacti-api.company.local;

    ssl_certificate /etc/letsencrypt/live/cacti-api.company.local/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/cacti-api.company.local/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    location / {
        limit_req zone=cacti_api_limit burst=60 nodelay;

        proxy_pass http://127.0.0.1:9191;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-ID $request_id; # Mengirimkan tracing ID dari Nginx

        # Buffer tuning untuk Grafana query berukuran besar
        proxy_buffering on;
        proxy_buffers 8 16k;
        proxy_buffer_size 16k;

        # Disable keepalive timeouts jika client terlalu sering putus
        keepalive_timeout 65;
    }
}
```

---

## 🔌 API Endpoints

### 1. `GET /api/v1/ping`
Mengembalikan status kesehatan server. Response: `"pong"`.

### 2. `GET /api/v1/list_metrics`
Mengembalikan daftar metrik RRD yang tersedia dalam format `[filename]:[ds_name]`.
*   **Query Parameter**: `glob` (opsional) untuk menyaring hasil (e.g., `/api/v1/list_metrics?glob=localhost_*`).

### 3. `GET /api/v1/xport`
Mengekspor data time-series dari file RRD dalam format JSON/XML.
*   **Query Parameter**:
    *   `start`: Waktu awal data (e.g., `-1h`, `-24h`, atau timestamp epoch).
    *   `end`: Waktu akhir data (e.g., `now` atau timestamp epoch).
    *   `step`: Interval data dalam detik (e.g., `300`).
    *   `xport`: Spesifikasi ekstraksi (e.g., `DEF:val=localhost_mem_buffers_3.rrd:mem_buffers:AVERAGE XPORT:val:mem_buffers`).
    *   `format`: Format keluaran (`json` atau `xml`, default: `json`).

### 4. `GET /api/v1/graph`
Menghasilkan grafik visual (SVG/PNG) langsung dari data RRD.
*   **Query Parameter**:
    *   `start`, `end`, `step` (sama seperti xport).
    *   `imgformat`: Format gambar (`SVG` atau `PNG`, default: `SVG`).
    *   `graph`: Spesifikasi grafik (e.g., `DEF:val=localhost_mem_buffers_3.rrd:mem_buffers:AVERAGE LINE1.5:val#38a169`).
    *   `title`: Judul grafik.

---

## 📊 Integrasi Grafana

Untuk menampilkan data RRD Cacti ke Grafana, gunakan plugin **Infinity** atau **JSON API** (direkomendasikan):

1.  **Install Plugin**:
    Buka Grafana dan instal plugin **JSON API** oleh Marcus Olsson.
2.  **Tambahkan Data Source**:
    - Pilih **JSON API** sebagai tipe Data Source.
    - Set **URL** ke alamat API server (e.g., `https://cacti-api.company.local` or `http://127.0.0.1:9191`).
    - Jika Basic Auth diaktifkan, konfigurasikan username & password di bagian Authentication.
3.  **Konfigurasikan Query di Panel Dashboard**:
    - Set **Path** ke `/api/v1/xport`.
    - Tambahkan **Query Parameters**:
        - `start` = `${__from:date:seconds}`
        - `end` = `${__to:date:seconds}`
        - `xport` = `DEF:val=localhost_mem_buffers_3.rrd:mem_buffers:AVERAGE XPORT:val:mem_buffers`
    - Di bagian **JSON Fields**, petakan data JSON yang diterima:
        - `$.data[*][0]` (Timestamp) -> Petakan sebagai *Time* (format Epoch/seconds).
        - `$.data[*][1][0]` (Value) -> Petakan sebagai *Number* (misal: Memory Usage).

---

## 🗄️ Resolusi Nama Interface dari Database Cacti

Secara default, file RRD hanya menyimpan ID data source internal (e.g. `localhost_traffic_in_4.rrd`). Nama interface yang ramah manusia (seperti `GigabitEthernet0/1` atau `eth0`) disimpan di database MariaDB/MySQL Cacti.

API ini secara opsional dapat tersambung ke database Cacti untuk menerjemahkan nama file RRD ke nama asli interface secara real-time.

### Cara Mengaktifkan:
Tambahkan kredensial database Cacti Anda ke dalam environment variables saat menjalankan server (atau gunakan CLI flags):

```bash
export RRD_DB_HOST="127.0.0.1"
export RRD_DB_PORT="3306"
export RRD_DB_USER="cactiuser"
export RRD_DB_PASS="cactipassword"
export RRD_DB_NAME="cacti"
```

Jika diaktifkan, Anda dapat memanggil endpoint `/api/v1/list_metrics?detail=true` untuk mendapatkan respons kaya metadata:

```json
[
  {
    "metric": "localhost_traffic_in_4.rrd:traffic_in",
    "file": "localhost_traffic_in_4.rrd",
    "ds": "traffic_in",
    "title": "Localhost - Traffic - eth0 (traffic_in)"
  }
]
```

Dashboard bawaan server (browser di `http://127.0.0.1:9191`) akan otomatis mendeteksi konfigurasi ini dan menampilkan nama antarmuka yang ramah manusia pada daftar metrik di sidebar secara otomatis!

