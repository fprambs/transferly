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

Los [datos originales](sftp-http-2026-11-27.json) incluyen tiempos por solicitud, duración total y tasa por lote. Esa primera ejecución mide una sola instancia, un usuario y un servidor SFTP en red local. El [experimento de RTT](../spikes/sftp-throughput.md) mide por separado SFTP bajo latencia y ancho de banda inyectados.

## Carga con varios usuarios

```sh
./scripts/bench-connections.sh --users 1 4 8 16 32 --rounds 4 > multi-user.json
```

Cada usuario tiene una cuenta sintética, su propia sesión HTTP y una conexión SFTP en su espacio personal. El aprovisionamiento usa invitaciones reales, respeta `Retry-After` y queda fuera de la medición. Durante la preparación se fija en cada espacio la clave del servidor SFTP del banco. Antes de cada lote se intenta calentar la conexión de cada usuario mediante un listado. Todos los usuarios de un lote empiezan juntos. Cada uno ejecuta cuatro ciclos de tres listados y una descarga de 16 MiB verificada por SHA-256, con una solicitud a la vez y sin pausas artificiales.

Los percentiles de cada operación usan las solicitudes exitosas. El informe separa los fallos de calentamiento de los de la carga medida y el script devuelve un estado distinto de cero si hubo errores en cualquiera de esas fases. La tasa de descarga se calcula sobre el tiempo completo del lote mixto. La prueba mide sesiones concurrentes en una sola instancia, con un servidor SFTP y una red local compartidos.

La corrida del 27 de noviembre usó el mismo entorno descrito arriba, con la aplicación en la revisión `9f2ddb0`. Los lotes duraron entre 1,24 y 6,78 segundos. Los [datos originales](sftp-http-multi-user-2026-11-27.json) incluyen las 976 solicitudes, los percentiles, los recuentos de errores y el tiempo de pared de cada lote.
