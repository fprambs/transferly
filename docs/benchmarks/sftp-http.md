# Rendimiento de la API con SFTP

Medición del 27 de noviembre de 2026. Transferly, PostgreSQL 17.12 y OpenSSH (`atmoz/sftp`) corrieron en una red de Docker Compose en una sola máquina. El cliente de carga también corrió en Docker y llamó a la API de Transferly. Entorno Linux x86-64 en WSL2 (kernel 6.18.58.1), AMD Ryzen AI 9 HX 370, 24 CPU lógicas y 15,6 GiB de RAM visibles. Backend Go 1.27.4 sobre la revisión `81dc79b`. No se inyectó latencia ni se limitó el ancho de banda.

## Reproducción

Desde la raíz del repositorio, con Docker Compose disponible:

```sh
./scripts/bench-connections.sh > connections.json
```

El script crea un proyecto desechable sin puertos publicados, una cuenta y datos sintéticos. Elige para la lista de redes permitidas únicamente la subred de ese proyecto. Limpia los contenedores y volúmenes de prueba al terminar. Aborta con un estado distinto de cero si falla una solicitud, cambia el tamaño o el SHA-256 de una descarga, o falta el archivo en el listado.

## Metodología

- **Conexión nueva:** se mide desde la creación de un sitio rápido hasta su cierre, incluyendo la primera operación de listado, que abre y autentica SSH/SFTP. Diez muestras por nivel. Se prueban hasta cuatro trabajadores porque el usuario tiene un límite de cinco conexiones rápidas y una está reservada para el escenario reutilizado.
- **Conexión reutilizada:** el mismo sitio ya conectado lista una carpeta de prueba pequeña. Ochenta muestras por nivel.
- **Descarga:** el mismo sitio sirve un archivo aleatorio de 16 MiB. Se realizan 16 descargas por nivel después de un calentamiento. Se incluye la lectura y comprobación de integridad del archivo en el cliente.
- **p50 y p95:** mediana y rango más próximo, respectivamente, del tiempo de cada operación desde que su trabajador la toma. Las tasas agregadas usan el tiempo de pared del lote. Con diez muestras, el p95 de conexiones nuevas es el máximo observado.

Los [datos originales](sftp-http-2026-11-27.json) incluyen tiempos por solicitud, duración total y tasa por lote. Esta ejecución mide una sola instancia, un usuario y un servidor SFTP en red local. Para caracterizar una instalación con red remota, múltiples usuarios, otros conectores o distinta configuración haz una medición específica. El [experimento de RTT](../spikes/sftp-throughput.md) mide por separado SFTP bajo latencia y ancho de banda inyectados.
