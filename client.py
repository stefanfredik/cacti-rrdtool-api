import hmac
import hashlib
import time
import requests
import re

SECRET = "5478b004f3f97a56a32b95a2c559fcf6bcfcd0e1712692839f32b3b4dea01ff9"
BASE_URL = "http://127.0.0.1:9292"
ENDPOINT = "/api/v1/graph"

expiry = str(int(time.time()) + 300)
graph_spec = "DEF:val=localhost_mem_buffers_3.rrd:mem_buffers:AVERAGE LINE1.5:val#38a169:mem_buffers"
raw_query = f"graph={graph_spec}&x={expiry}"

# HMAC
message = f"{ENDPOINT}?{raw_query}&".encode('utf-8')
signature = hmac.new(SECRET.encode('utf-8'), message, hashlib.sha256).hexdigest()

# Call with params
params = {
    "graph": graph_spec,
    "x": expiry,
    "s": signature
}
print(f"Mengakses: {BASE_URL}{ENDPOINT}")

response = requests.get(f"{BASE_URL}{ENDPOINT}", params=params)
if response.status_code == 200:
    svg = response.text
    paths = re.findall(r'<path[^>]*>', svg)
    print("Paths found in SVG:")
    for p in paths:
        print("  ", p)
else:
    print(f"Gagal: {response.status_code}, {response.text}")
