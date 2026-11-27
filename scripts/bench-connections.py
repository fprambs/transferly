"""Measure the HTTP API against the isolated Docker Compose SFTP service."""

from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import math
import statistics
import sys
import time
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


BASE = "http://transferly:8080"
PAYLOAD_PATH = "/upload/connection-bench.bin"
PAYLOAD_BYTES = 16 * 1024 * 1024
CREDENTIALS = {"email": "benchmark@example.invalid", "password": "synthetic benchmark password"}
CONNECTION = {
    "connector": "sftp",
    "config": {"host": "sftp", "port": "22", "username": "transferly", "auth": "password"},
    "secrets": {"password": "transferly"},
}


class Client:
    """Authenticate once, then issue independent HTTP requests from each worker."""

    def __init__(self):
        self.headers = {"Origin": BASE}
        with self.request("/setup", method="POST", payload=CREDENTIALS) as response:
            self.headers["Cookie"] = response.headers["Set-Cookie"].split(";", 1)[0]
            self.headers["X-CSRF-Token"] = json.load(response)["csrfToken"]
        with self.request("/vault/setup", "POST", {"masterPassword": "synthetic benchmark master password"}):
            pass
        self.workspace = "/workspaces/" + self.read("/workspaces")[0]["id"]
        self.quick = self.workspace + "/quick-connections"

    def request(self, path, method="GET", payload=None):
        """Open a request and let any HTTP failure stop the benchmark."""
        headers = self.headers.copy()
        data = None
        if payload is not None:
            headers["Content-Type"] = "application/json"
            data = json.dumps(payload).encode()
        return urlopen(Request(BASE + "/api/v1" + path, data=data, headers=headers, method=method), timeout=60)

    def read(self, path, method="GET", payload=None):
        """Decode a successful JSON response."""
        with self.request(path, method, payload) as response:
            return json.load(response)

    def connect(self):
        """Create a new quick site whose first file request will open the SSH connection."""
        try:
            return self.read(self.quick, "POST", CONNECTION)["id"]
        except HTTPError as error:
            with error:
                raise RuntimeError(f"creating quick connection: HTTP {error.code}: {error.read().decode()}") from error

    def disconnect(self, site):
        """Release a quick site's server-side session."""
        with self.request(f"{self.quick}/{site}", "DELETE"):
            pass

    def files(self, site, endpoint="", path="/upload"):
        """Address a file endpoint for a specific quick site."""
        return f"{self.workspace}/sites/{site}/files{endpoint}?{urlencode({'path': path})}"

    def listing(self, site):
        """Verify that the listing returns the benchmark file."""
        if not any(entry["name"] == "connection-bench.bin" for entry in self.read(self.files(site))):
            raise ValueError("SFTP listing did not include the benchmark file")

    def trust(self, site):
        """Approve the initial host key on the disposable test instance."""
        try:
            self.listing(site)
        except HTTPError as error:
            with error:
                problem = json.load(error)
            if error.code != 409 or problem.get("code") != "host_key_unknown":
                raise
            key = problem["params"]
            with self.request(self.workspace + "/known-hosts", "POST", {
                "host": "sftp", "port": 22, "keyType": key["keyType"], "fingerprint": key["fingerprint"],
            }):
                pass
            self.listing(site)

    def download(self, site, expected):
        """Stream and verify the size and SHA-256 of one download."""
        digest = hashlib.sha256()
        count = 0
        with self.request(self.files(site, "/content", PAYLOAD_PATH)) as response:
            while block := response.read(256 * 1024):
                count += len(block)
                digest.update(block)
        if count != PAYLOAD_BYTES or digest.hexdigest() != expected:
            raise ValueError(f"download integrity check failed ({count} bytes)")
        return count


def measure(operation, workers, samples, action):
    """Return per-request samples, aggregate rate and nearest-rank p95."""
    def timed(_):
        """Measure execution time without counting time queued in the executor."""
        started = time.perf_counter()
        size = action()
        return time.perf_counter() - started, size or 0

    print(f"{operation}: {samples} requests, {workers} workers", file=sys.stderr)
    started = time.perf_counter()
    with ThreadPoolExecutor(max_workers=workers) as pool:
        completed = list(pool.map(timed, range(samples)))
    elapsed = time.perf_counter() - started
    durations = sorted(duration * 1000 for duration, _ in completed)
    total = sum(size for _, size in completed)
    return {
        "operation": operation, "workers": workers, "samples": samples, "errors": 0,
        "elapsed_seconds": round(elapsed, 6), "operations_per_second": round(samples / elapsed, 2),
        "p50_ms": round(statistics.median(durations), 2),
        "p95_ms": round(durations[math.ceil(samples * 0.95) - 1], 2),
        "mib_per_second": round(total / (1024 ** 2) / elapsed, 2) if total else None,
        "samples_ms": [round(duration * 1000, 3) for duration, _ in completed],
    }


def main(expected):
    """Measure cold connections, reused connections and concurrent downloads from one site."""
    client = Client()

    def cold_listing():
        """Open and close a fresh site around its initial listing."""
        site = client.connect()
        try:
            client.listing(site)
        finally:
            client.disconnect(site)

    warm = client.connect()
    try:
        client.trust(warm)
        client.download(warm, expected)
        measurements = [
            measure("cold_sftp_listing", workers, 10, cold_listing) for workers in (1, 2, 4)
        ]
        for workers in (1, 4, 8):
            measurements.append(measure("reused_sftp_listing", workers, 80, lambda: client.listing(warm)))
            measurements.append(measure("sftp_download", workers, 16, lambda: client.download(warm, expected)))
        print(json.dumps({"payload_bytes": PAYLOAD_BYTES, "measurements": measurements}, indent=2))
    finally:
        client.disconnect(warm)


if __name__ == "__main__":
    main(sys.argv[1])
