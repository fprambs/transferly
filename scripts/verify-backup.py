"""Exercise cold vault recovery and an encrypted SFTP credential in the isolated restoration stack."""
import http.cookiejar
import http.client
import hashlib
import gzip
import json
import pathlib
import os
import statistics
import subprocess
import re
import sys
import socket
import uuid
import time
import urllib.error
import urllib.request

base = "http://app:8080/api/v1"
credentials = {"email": "restore@example.invalid", "password": "synthetic restoration password"}
master = "synthetic restoration master password"
jar = http.cookiejar.CookieJar()
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
csrf = ""


def call(method, path, body=None, binary=False):
    """Send a fixture request without printing tokens, recovery kits or decrypted credentials."""
    headers = {"Content-Type": "application/octet-stream" if binary else "application/json"}
    if csrf:
        headers["X-CSRF-Token"] = csrf
    data = body if binary else None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    with client.open(req, timeout=30) as response:
        raw = response.read()
        return raw if binary else None if not raw else json.loads(raw)


mode = sys.argv[1]
state_path = pathlib.Path(sys.argv[2] if len(sys.argv) > 2 else "/reports/restore-state.json")
if mode == "bundle":
    with client.open("http://app:8080/", timeout=30) as response:
        html = response.read(1024 * 1024).decode()
    assets = set(re.findall(r'(?:src|href)="(/assets/[^\"]+\.js)"', html))
    assert assets, "Missing initial JavaScript graph"
    total = 0
    for asset in assets:
        with client.open("http://app:8080" + asset, timeout=30) as response:
            data = response.read(8 * 1024 * 1024 + 1)
            assert len(data) <= 8 * 1024 * 1024, "Asset exceeds measurement cap"
            total += len(gzip.compress(data, compresslevel=6, mtime=0))
    print(json.dumps({"initial_gzip_bytes": total, "initial_js_files": len(assets)}))
elif mode == "seed":
    session = call("POST", "/setup", credentials)
    csrf = session["csrfToken"]
    call("POST", "/vault/setup", {"masterPassword": master})
    workspace = call("GET", "/workspaces")[0]["id"]
    site = call("POST", f"/workspaces/{workspace}/sites", {
        "name": "Restoration sentinel", "connector": "sftp",
        "config": {"host": "sftp", "port": "22", "username": "transferly", "auth": "password"},
        "secrets": {"password": "transferly"},
    })["id"]
    files = f"/workspaces/{workspace}/sites/{site}/files"
    for attempt in range(30):
        try:
            call("GET", files + "?path=%2F")
            break
        except urllib.error.HTTPError as error:
            problem = json.loads(error.read())
            if error.code == 409 and "params" in problem:
                params = problem["params"]
                call("POST", f"/workspaces/{workspace}/known-hosts", {"host": "sftp", "port": 22, "keyType": params["keyType"], "fingerprint": params["fingerprint"]})
            elif attempt == 29:
                raise
            else:
                time.sleep(1)
    call("PUT", files + "/content?path=%2Fupload%2Frestore-sentinel.txt", b"restored encrypted credential", True)
    state = {"workspace": workspace, "site": site}
    if "--legacy" in sys.argv:
        transfer = str(uuid.uuid4())
        connection = http.client.HTTPConnection("app", 8080, timeout=30)
        connection.putrequest("PUT", f"/api/v1{files}/content?path=%2Fupload%2Flegacy-resume.bin&transferId={transfer}")
        connection.putheader("Content-Type", "application/octet-stream")
        connection.putheader("Content-Length", "32")
        connection.putheader("X-CSRF-Token", csrf)
        connection.putheader("Cookie", "; ".join(f"{cookie.name}={cookie.value}" for cookie in jar))
        connection.endheaders()
        connection.send(b"legacy prefix")
        connection.sock.shutdown(socket.SHUT_WR)
        response = connection.getresponse()
        assert response.status >= 400, response.status
        response.read()
        connection.close()
        assert call("GET", files + "/content?path=%2Fupload%2Flegacy-resume.bin.part", binary=True) == b"legacy prefix"
        state["legacyTransfer"] = transfer
    state_path.write_text(json.dumps(state))
    print("Seeded a synthetic vault, trusted server and file")
elif mode in ("check", "check-upgrade", "bench"):
    session = call("POST", "/auth/login", credentials)
    csrf = session["csrfToken"]
    call("POST", "/vault/unlock", {"masterPassword": master})
    state = json.loads(state_path.read_text())
    raw = call("GET", f"/workspaces/{state['workspace']}/sites/{state['site']}/files/content?path=%2Fupload%2Frestore-sentinel.txt", binary=True)
    assert raw == b"restored encrypted credential"
    if "legacyTransfer" in state:
        files = f"/workspaces/{state['workspace']}/sites/{state['site']}/files"
        assert call("GET", files + "/content?path=%2Fupload%2Flegacy-resume.bin.part", binary=True) == b"legacy prefix"
        if mode == "check-upgrade":
            try:
                call("PUT", files + f"/content?path=%2Fupload%2Flegacy-resume.bin&transferId={state['legacyTransfer']}&mode=resume&offset=13", b"remaining bytes", True)
                raise AssertionError("Adopted unowned legacy staging")
            except urllib.error.HTTPError as error:
                assert error.code == 404 and json.loads(error.read())["code"] == "path_not_found"
            assert call("GET", files + "/content?path=%2Fupload%2Flegacy-resume.bin.part", binary=True) == b"legacy prefix"
            print("Upgrade preserved legacy staging and refused unowned resume")
    print("Cold restoration verified: account, vault, trusted key, decrypted SFTP credential and file bytes")
    if mode == "bench":
        askpass = pathlib.Path("/tmp/transferly-bench-askpass")
        askpass.write_text("#!/bin/sh\necho transferly\n")
        askpass.chmod(0o700)
        native = pathlib.Path("/tmp/transferly-bench-native.bin")
        native_times, api_times, listing_times = [], [], []
        trials = int(os.environ.get("TRANSFERLY_BENCH_TRIALS", "3"))
        assert 3 <= trials <= 20, "Choose between 3 and 20 measured trials"
        files = f"/workspaces/{state['workspace']}/sites/{state['site']}/files"
        for trial in range(trials + 1):
            start = time.perf_counter()
            subprocess.run(["sftp", "-q", "-oStrictHostKeyChecking=no", "-oUserKnownHostsFile=/dev/null", "transferly@sftp:/upload/bench.bin", str(native)],
                           stdin=subprocess.DEVNULL, capture_output=True, check=True,
                           env={**os.environ, "SSH_ASKPASS": str(askpass), "SSH_ASKPASS_REQUIRE": "force", "DISPLAY": ":99"}, timeout=60)
            elapsed_native = time.perf_counter() - start
            with native.open("rb") as source:
                digest_native = hashlib.file_digest(source, "sha256").hexdigest()
            digest_api, total = hashlib.sha256(), 0
            start = time.perf_counter()
            with client.open(base + files + "/content?path=%2Fupload%2Fbench.bin", timeout=60) as response:
                for chunk in iter(lambda: response.read(65536), b""):
                    digest_api.update(chunk)
                    total += len(chunk)
                    assert total <= 64 * 1024 * 1024, "Unexpected benchmark size"
            elapsed_api = time.perf_counter() - start
            assert total == 64 * 1024 * 1024 and digest_api.hexdigest() == digest_native, "Benchmark bytes differ"
            start = time.perf_counter()
            for _ in range(20):
                call("GET", files + "?path=%2Fupload")
            elapsed_listing = (time.perf_counter() - start) / 20
            if trial:
                native_times.append(elapsed_native)
                api_times.append(elapsed_api)
                listing_times.append(elapsed_listing)
        print(json.dumps({"mib": 64, "trials": trials, "native_mib_s": round(64 / statistics.median(native_times), 2),
                          "api_mib_s": round(64 / statistics.median(api_times), 2),
                          "listing_median_ms": round(statistics.median(listing_times) * 1000, 2),
                          "api_samples_s": [round(value, 4) for value in api_times]}))
else:
    raise SystemExit("Choose seed, check, check-upgrade, bundle or bench")
