import hmac
import hashlib
import time
import requests

SECRET = "5478b004f3f97a56a32b95a2c559fcf6bcfcd0e1712692839f32b3b4dea01ff9"
BASE_URL = "http://127.0.0.1:9292"
ENDPOINT = "/api/v1/graph"

# Mengatur kedaluwarsa token (contoh: berlaku untuk 5 menit ke depan)
expiry = str(int(time.time()) + 300)

# Tentukan argumen grafik/RRD yang ingin diambil dari Cacti
# Contoh format: graph=nama_file_rrd.rrd&options...
query_params = f"graph=localhost_mem_buffers_3.rrd&x={expiry}"

# Menghitung HMAC-SHA256 sesuai standarisasi repo rrdsrv
# Format kalkulasi: path || "?" || query-params || "&"
message = f"{ENDPOINT}?{query_params}&".encode('utf-8')
signature = hmac.new(SECRET.encode('utf-8'), message, hashlib.sha256).hexdigest()

# Membentuk URL final dengan Auth Token terlampir
final_url = f"{BASE_URL}{ENDPOINT}?{query_params}&s={signature}"

print(f"Mengakses URL Ter-Autentikasi: {final_url}")

# Eksekusi request
response = requests.get(final_url)
if response.status_code == 200:
    print("Koneksi Sukses! Data grafik berhasil diambil.")
    # Hasil output default dari rrdsrv /api/v1/graph berupa SVG/PNG data
else:
    print(f"Gagal terhubung. Status code: {response.status_code}, Detail: {response.text}")
