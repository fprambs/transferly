# Compatibilidad SSH para la versión estable

Se conserva `diffie-hellman-group-exchange-sha256` y `TestDial_GroupExchangeSHA256`, agregados durante la investigación de `gf-sftp.sfg.bnymellon.com:2222`. Esta extensión se prueba contra servidores locales en proceso; no envía credenciales a ese servidor.

## Política de algoritmos

La referencia es la dependencia instalada `golang.org/x/crypto v0.57.0`, especialmente [SupportedAlgorithms e InsecureAlgorithms](https://pkg.go.dev/golang.org/x/crypto@v0.57.0/ssh#SupportedAlgorithms).

| Categoría | Política |
| --- | --- |
| Intercambio de claves | ML-KEM768/X25519, Curve25519 y su alias libssh, ECDH P-256/P-384/P-521, DH group14 SHA-256, group16 SHA-512 y group-exchange SHA-256. Una prueba compara la lista con todos los algoritmos seguros de la dependencia. |
| Cifrado | ChaCha20-Poly1305, AES CTR de 128/192/256 bits y AES GCM de 128/256 bits. Se conserva el orden existente por compatibilidad con gateways empresariales. |
| MAC | Lista segura explícita de la dependencia: HMAC SHA-256/SHA-512, versiones encrypt-then-MAC y HMAC SHA-1 completo. Se excluye HMAC SHA-1 truncado a 96 bits. HMAC SHA-1 completo pertenece al conjunto seguro de la dependencia; no equivale a firmas RSA/SHA-1. |
| Firma del servidor | Lista segura de la dependencia intersectada con los algoritmos de las identidades previamente confiadas. RSA usa SHA-256/SHA-512, nunca la firma `ssh-rsa` SHA-1. Una intersección vacía falla cerrada, sin volver a los valores predeterminados. |
| Firma de autenticación | Se restringe el firmante a la lista segura; RSA usa SHA-256/SHA-512 y no puede volver a SHA-1. |

Los algoritmos de `InsecureAlgorithms()` siguen deshabilitados. No hay fallback automático ni interruptor global. El servidor reportado no necesita criptografía obsoleta. Por eso esta etapa no incorpora una excepción por sitio. Si aparece una necesidad concreta, debe resolverse mediante una opción por sitio, apagada por defecto, con advertencia de riesgo y algoritmos limitados al caso requerido; nunca relajando esta política para todos los sitios.

## Diagnóstico y reintentos

`remotefs.NegotiationError` conserva una categoría estable y los nombres anunciados por el servidor. `errors.As` reconoce el error tipado de SSH aun envuelto por el handshake. Las categorías son `key_exchange`, `cipher`, `mac`, `host_signature` y `authentication_signature`.

HTTP devuelve 422 con código `ssh_algorithm_incompatible` y parámetros `category` y `serverAlgorithms`, en vez de `remote_unavailable`/502. La guía de errores muestra categoría traducida, algoritmos y acción recomendada en español e inglés. Los nombres remotos se limitan a 64 entradas de 128 caracteres y se eliminan controles antes de exponerlos. La interfaz los muestra como texto.

El error no incluye `ErrConnectionLost`; `Retryable` también rechaza expresamente la incompatibilidad si viene combinada con otro error. Una prueba del gestor verifica un solo intento, sin backoff. Las conexiones a jump hosts pasan por la misma política y clasificación.

La dependencia tiene un diagnóstico antiguo no tipado para el fallback de firmas de autenticación RSA. Se reconoce exclusivamente su formato fijo. En ese caso la biblioteca informa la firma de fallback (`ssh-rsa`), no necesariamente el anuncio completo `server-sig-algs`; esa limitación no afecta los anuncios completos de intercambio, cifrado, MAC o firma de servidor.

## Validación

- Suites de SFTP, connect, remotefs, connections y HTTP con `go test -race`: pasan.
- `golangci-lint` de los paquetes afectados: 0 problemas.
- Pruebas de servidores con KEX/cifrado/MAC incompatibles: categoría y algoritmos correctos, sin sesión SFTP ni reintento.
- Todos los MAC seguros con AES CTR: conexión exitosa.
- RSA SHA-256/SHA-512: conexión exitosa para firma de servidor y autenticación; RSA SHA-1: rechazado con diagnóstico.
- Se mantiene la regresión de group-exchange SHA-256 y la cobertura previa de cifrados modernos/obsoletos.
- Comprobación del contrato HTTP 422 y parámetros del diagnóstico.
- Tipos, lint y pruebas de la guía frontend: pasan (registro `/tmp/transferly-ssh-ui-final.log`).

Esta validación corresponde a esta extensión. No vuelve a afirmar el 100% de cobertura de la etapa anterior ni reemplaza la validación de CI remoto pendiente antes de publicar.


## Corrección posterior: timeout y ciclo de reconexión

Se reportaron ocho intentos TCP a intervalos de unos 15 segundos. Se encontró una ruta en `Manager.await` que descartaba los errores `context.DeadlineExceeded` y `context.Canceled` del conector, tratándolos como abandono del dial compartido; una adquisición activa podía volver a iniciar el ciclo. Ahora se distingue la cancelación real del contexto compartido del error devuelto por el conector. Solo un dial realmente abandonado permite volver a consultar el pool sin devolver su error.

Un timeout TCP de SFTP devuelve `ErrConnectTimeout`, no reintentable incluso combinado con `ErrConnectionLost`. HTTP lo representa como 504 / `remote_connection_timeout`. La interfaz explica que el servidor no respondió y que puede tratarse de un bloqueo, red o servidor no disponible; no afirma que el timeout demuestre un bloqueo. El usuario puede reintentar manualmente después de revisar acceso, host y puerto. Los fallos de red restantes conservan el máximo configurado de intentos.

Regresiones: deadline/cancelación originados por el conector se devuelven tras un solo intento; timeout TCP no alcanza validación de clave ni reintenta; contrato HTTP 504 y guía en español. Se mantienen las pruebas de abandono, adquisiciones compartidas y cancelación anteriores. Registros: `/tmp/transferly-timeout-go.log` y `/tmp/transferly-timeout-ui.log`.


## Gate completo del candidato

La revalidación posterior cubre también esta extensión: Go oficial con detector de carreras y 100 % por función, frontend completo con 1.296 pruebas y 100 % en las cuatro métricas, lint/tipos y 163 pruebas de navegador sobre la imagen reconstruida, sin retries. Se completaron pruebas de límites del anuncio remoto, firmantes sin restricciones, claves DSA rechazadas antes de dial y diagnósticos incompletos. La [validación actualizada](2026-10-09-validation-results.md) reemplaza la limitación anterior sobre cobertura no revalidada. CI del commit definitivo y publicación continúan pendientes.
