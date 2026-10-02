#!/usr/bin/env bash
# Starts the dev SFTP server with preview samples and prints connection details for the web UI.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

compose=(docker compose -f compose.dev.yaml --profile testenv)

echo "→ Starting SFTP test server (preview samples)…"
"${compose[@]}" up -d sftp

echo "→ Waiting for SFTP to become healthy…"
deadline=$((SECONDS + 60))
while true; do
  status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' transferly-dev-sftp-1 2>/dev/null || echo missing)"
  if [[ "$status" == "healthy" ]]; then
    break
  fi
  if (( SECONDS > deadline )); then
    echo "SFTP did not become healthy in time (last status: $status)." >&2
    exit 1
  fi
  sleep 1
done

echo "→ Checking that the backend can reach SFTP…"
if ! docker compose -f compose.dev.yaml run --rm -T tools sh -c \
  'cd backend && TRANSFERLY_TEST_SFTP_ADDR=sftp:22 go test -count=1 -run TestConformance_OpenSSH ./internal/adapters/remotefs/sftpfs/' \
  >/dev/null 2>&1; then
  echo "Warning: OpenSSH conformance test failed; is transferly-dev-backend-1 running?" >&2
else
  echo "   OK (password auth, upload dir, preview mount)."
fi

web_port="${TRANSFERLY_WEB_PORT:-5173}"

cat <<EOF

════════════════════════════════════════════════════════════════════════
  SFTP de prueba (Transferly) — solo red Docker de desarrollo
════════════════════════════════════════════════════════════════════════

  Las muestras están en testenv/preview-samples, montadas en el servidor
  como carpeta remota /preview-samples (solo lectura). Tu SFTP real no se toca.

  1. Abre Transferly en http://localhost:${web_port} e inicia sesión.
  2. Conexión rápida (Ctrl+Shift+K) o guarda un sitio nuevo con:

       Protocolo   SFTP
       Host        sftp
       Puerto      22
       Usuario     transferly
       Contraseña  transferly

     Usa el host sftp (no localhost): la conexión la hace el backend dentro
     de Docker; localhost desde el navegador no llegaría al contenedor.

  3. Tras conectar, abre /preview-samples para probar visores (PDF, imágenes,
     archivos, etc.). /upload es escribible si quieres subir pruebas.

  Apagar solo SFTP: docker compose -f compose.dev.yaml --profile testenv stop sftp

════════════════════════════════════════════════════════════════════════

EOF
