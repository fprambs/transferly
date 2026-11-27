"""Measure the HTTP API against the isolated Docker Compose SFTP service."""

import argparse
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
import hashlib
from http.client import HTTPException
import json
import math
import statistics
import sys
from threading import Barrier
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

    def __init__(self, token=None):
        """Start a new session through setup or a single-use team invitation."""
        self.headers = {"Origin": BASE}
        path, payload = ("/setup", CREDENTIALS) if token is None else (
            "/invitations/join", {"token": token, "password": CREDENTIALS["password"]},
        )
        for attempt in range(20):
            try:
                response = self.request(path, "POST", payload)
                break
            except HTTPError as error:
                with error:
                    if token is None or error.code != 429 or attempt == 19:
                        raise
                    time.sleep(max(1, int(error.headers["Retry-After"])))
        with response:
            self.headers["Cookie"] = response.headers["Set-Cookie"].split(";", 1)[0]
            self.headers["X-CSRF-Token"] = json.load(response)["csrfToken"]
        with self.request("/vault/setup", "POST", {"masterPassword": "synthetic benchmark master password"}):
            pass
        personal = next(workspace for workspace in self.read("/workspaces") if workspace["kind"] == "personal")
        self.workspace = "/workspaces/" + personal["id"]
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
        """Approve and return the initial host key on the disposable test instance."""
        try:
            self.listing(site)
        except HTTPError as error:
            with error:
                problem = json.load(error)
            if error.code != 409 or problem.get("code") != "host_key_unknown":
                raise
            key = problem["params"]
            self.trust_host(key)
            self.listing(site)
            return key
        return None

    def trust_host(self, key):
        """Pin the same isolated server's key in this user's workspace."""
        with self.request(self.workspace + "/known-hosts", "POST", {
            "host": "sftp", "port": 22, "keyType": key["keyType"], "fingerprint": key["fingerprint"],
        }):
            pass

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


def simulated_users(count):
    """Provision separate users, sessions and personal workspaces before applying load."""
    print(f"provisioning {count} accounts before timing", file=sys.stderr)
    admin = Client()
    users = [admin]
    if count > 1:
        team = admin.read("/workspaces", "POST", {"name": "Benchmark onboarding"})
        for number in range(2, count + 1):
            invite = admin.read(f"/workspaces/{team['id']}/invitations", "POST", {
                "email": f"benchmark-{number}@example.invalid", "role": "viewer",
            })
            users.append(Client(invite["token"]))
    return users


def mixed_load(actors, rounds, expected):
    """Release all users together, alternating three listings and a verified download."""
    ready = Barrier(len(actors) + 1)

    def workload(actor):
        """Run one user's requests in order and retain failures as measured outcomes."""
        client, site = actor
        results = []
        ready.wait()
        for operation in ("listing", "listing", "listing", "download") * rounds:
            started = time.perf_counter()
            error = None
            try:
                if operation == "listing":
                    client.listing(site)
                else:
                    client.download(site, expected)
            except HTTPError as failure:
                error = http_failure(failure)
            except (HTTPException, OSError, ValueError) as failure:
                error = type(failure).__name__
            results.append({
                "operation": operation, "ms": round((time.perf_counter() - started) * 1000, 3),
                "error": error,
            })
        return results

    print(f"mixed SFTP load: {len(actors)} users, {rounds} rounds each", file=sys.stderr)
    with ThreadPoolExecutor(max_workers=len(actors)) as pool:
        pending = [pool.submit(workload, actor) for actor in actors]
        started = time.perf_counter()
        ready.wait()
        results = [item for task in pending for item in task.result()]
        elapsed = time.perf_counter() - started
    failures = Counter(item["error"] for item in results if item["error"])
    summary = {}
    for operation in ("listing", "download"):
        durations = sorted(item["ms"] for item in results if item["operation"] == operation and not item["error"])
        summary[operation] = {
            "completed": len(durations),
            "p50_ms": round(statistics.median(durations), 2) if durations else None,
            "p95_ms": round(durations[math.ceil(len(durations) * 0.95) - 1], 2) if durations else None,
        }
    return {
        "users": len(actors), "elapsed_seconds": round(elapsed, 6), "attempted": len(results),
        "errors": dict(failures), "results": summary,
        "operations_per_second": round((len(results) - sum(failures.values())) / elapsed, 2),
        "download_mib_per_second": round(summary["download"]["completed"] * PAYLOAD_BYTES / (1024 ** 2) / elapsed, 2),
        "samples": results,
    }


def http_failure(failure):
    """Preserve the HTTP status and the API problem code without logging the response body."""
    with failure:
        try:
            code = json.load(failure).get("code")
        except (HTTPException, OSError, ValueError):
            code = None
    return f"http_{failure.code}:{code}" if code else f"http_{failure.code}"


def multi_user(expected, levels, rounds):
    """Measure each requested concurrency level against one shared OpenSSH server."""
    actors = []
    try:
        for client in simulated_users(max(levels)):
            site = client.connect()
            actors.append((client, site))
        admin, first_site = actors[0]
        key = admin.trust(first_site)
        for client, _ in actors[1:]:
            client.trust_host(key)
        results = []
        for level in levels:
            active = actors[:level]
            warmup_errors = Counter()
            for client, site in active:
                try:
                    client.listing(site)
                except HTTPError as failure:
                    warmup_errors[http_failure(failure)] += 1
                except (HTTPException, OSError, ValueError) as failure:
                    warmup_errors[type(failure).__name__] += 1
            result = mixed_load(active, rounds, expected)
            result["warmup_errors"] = dict(warmup_errors)
            results.append(result)
        print(json.dumps({"payload_bytes": PAYLOAD_BYTES, "rounds_per_user": rounds, "measurements": results}, indent=2))
        return any(result["errors"] or result["warmup_errors"] for result in results)
    finally:
        for client, site in actors:
            client.disconnect(site)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("checksum")
    parser.add_argument("--users", type=int, nargs="+", help="concurrent accounts for each mixed-load stage")
    parser.add_argument("--rounds", type=int, default=4, help="mixed-load rounds per user")
    options = parser.parse_args()
    if options.users is None:
        main(options.checksum)
    else:
        if not 1 <= min(options.users) <= max(options.users) <= 64 or options.rounds < 1:
            parser.error("use 1–64 users per stage and at least one round")
        sys.exit(multi_user(options.checksum, options.users, options.rounds))
