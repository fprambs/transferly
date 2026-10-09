"""Verify actual trusted TLS, secure authentication cookies and origin enforcement in the isolated Caddy stack."""
import http.client
import json
import socket
import ssl


class LocalTLS(http.client.HTTPSConnection):
    """Connect through the internal service name while validating localhost as the certificate identity."""
    def connect(self):
        self.sock = self._context.wrap_socket(socket.create_connection(("caddy", 443), timeout=30), server_hostname="localhost")


client = LocalTLS("localhost", context=ssl.create_default_context(cafile="/reports/caddy-root.crt"), timeout=30)
client.request("GET", "/api/v1/setup/status")
response = client.getresponse()
assert response.status == 200, response.status
response.read()
credentials = json.dumps({"email": "https@example.invalid", "password": "synthetic HTTPS password"})
headers = {"Content-Type": "application/json", "Origin": "https://localhost"}
client.request("POST", "/api/v1/setup", credentials, headers)
response = client.getresponse()
if response.status == 409:
    response.read()
    client.request("POST", "/api/v1/auth/login", credentials, headers)
    response = client.getresponse()
assert response.status in (200, 201), response.status
cookie = response.getheader("Set-Cookie")
assert cookie and "Secure" in cookie and "HttpOnly" in cookie and "SameSite=Lax" in cookie, "Incorrect cookie attributes"
assert response.getheader("Strict-Transport-Security"), "Missing HSTS"
session = json.loads(response.read())
client.request("POST", "/api/v1/auth/logout", headers={"Cookie": cookie.split(";", 1)[0], "X-CSRF-Token": session["csrfToken"], "Origin": "https://untrusted.example.invalid"})
response = client.getresponse()
assert response.status == 403, response.status
response.read()
client.close()
print("Verified TLS certificate, HTTPS authentication, Secure/HttpOnly/Lax cookies, HSTS and origin rejection")
