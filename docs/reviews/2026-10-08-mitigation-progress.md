# Estado de implementación y validación

Seguimiento del [plan de mitigación](2026-10-08-mitigation-plan.md), sobre la base `ba2e4db`. Los cambios están en el árbol de trabajo; no se han publicado imágenes, tags ni commits remotos.

**Todavía no es un candidato estable aprobado.** Las comprobaciones locales que pasaron se distinguen de las pruebas de navegador, actualización, rendimiento y CI que deben terminar antes de publicar. No se redujeron los umbrales de cobertura.

| Hallazgo | Implementación local | Validación y trabajo restante |
| --- | --- | --- |
| R01 | Registro y revocación de terminales por sesión/usuario/workspace; revalidación de expiración, permisos y comandos | Regresiones del router y carreras de apertura pasan. Completar navegador con SSH real. |
| R02 | Reemplazo opcional seguro, preflight, staging por UUID y exclusión por destino normalizado | Preservación de bytes, fallos de commit, concurrencia y extensión SFTP probados. Falta terminar conformance de navegador y ensayo del staging anterior; limpieza remota permanece deliberada. |
| R03 | Revocación de requests/sockets, limpieza central de estado privado y señal entre pestañas | Pruebas de respuestas y eventos tardíos, identidad inicial y almacenamiento entre pestañas. Completar navegador con varias cuentas. |
| R04 | Origen público HTTPS coherente, Caddy con IP específica, normalización IP→CIDR | TLS con certificado validado, setup/login, cookies Secure/HttpOnly/Lax, HSTS y rechazo de origen ajeno pasan en instalación aislada. |
| R05 | CLI shadcn retirada, CSS MIT conservado, dependencia afectada actualizada | `pnpm audit` completo sin avisos. `govulncheck`: sin vulnerabilidades llamadas/importadas; un aviso en módulo requerido sin uso vulnerable. |
| R06 | Un script inicia todos los protocolos para Make/CI; cobertura de copia Azure recuperada | Suite Go final con detector de carreras: 100 %; cancelación de locks repetida 20 veces. CI remoto aún no ejecutado. |
| R07 | Reservas de sitios ordenadas y cancelables; copia streaming con una conexión se rechaza | Regresión de presupuesto pasa; se están repitiendo las cancelaciones de reservas y verificando su liberación. |
| R08 | Comparación de rutas normalizadas para copia y locks de subida | Alias de self-copy/descendencia y escritura concurrente probados. La normalización no resuelve symlinks ni sitios equivalentes. |
| R09 | Identidad de preview incluye versión de raíz local y stamps remotos; caché se elimina al cerrar | Pruebas de aislamiento y invalidación; navegador pendiente. |
| R10 | Recorrido compartido con presupuesto de entradas/profundidad/tiempo y señal de cancelación | Árboles de directorios vacíos, rutas inseguras, profundidad y cancelación probados. Entradas locales cuentan también directorios. |
| R11 | Editor limita bytes reales; metadata/listados/errores remotos tienen límites y deadlines | Fixtures de metadata/errores excesivos y streams con cancelación defectuosa probados. |
| R12 | ZIP preflight, fork acotado de sevenzip y verificación de expansión Office previa al parser | ZIP/ZIP64 corruptos, presupuestos de codecs/diccionario y Office probados; archivos normales y cifrados conservan soporte. Fork documentado con licencia y prueba CI propia. |
| R13 | Versiones SHA/main y SemVer, imagen de release no reemplazable, SBOM y provenance | Workflow preparado; actualización y rollback desde 0.9.0 verificados. Publicación remota no ejecutada. |
| R14 | Editor, terminal, settings y visores se cargan bajo demanda; límite del grafo inicial en CI | Main ~222,5 kB gzip frente a ~723 kB. Medición del grafo completo previa ~486,7 kB gzip; repetir build final y benchmarks. Descargas y movimiento del árbol se separaron en controladores propios; se está revalidando la extracción. |
| R15 | Acciones fallidas visibles; persistencia con diagnóstico y reintento final acotado | Pruebas de API y recuperación de escritura final. Un fallo persistente requiere reconciliación del restart y verificar el archivo remoto antes de repetir. |
| R16 | Cancelación de queries antes del disconnect y reconciliación del indicador | Regresiones unitarias pasan; faltan las repeticiones del navegador y dos ejecuciones completas sin flaky. |

## Evidencia operativa

- Gitleaks: 614 commits revisados; los 30 resultados originales eran claves de fixtures/emuladores, ejemplos de test y fragmentos de OpenAPI generado. Excepciones de fixtures por fingerprint y excepción del OpenAPI limitada a literales base64 consecutivos del archivo generado, con el YAML original escaneado; nuevo escaneo sin filtraciones detectadas. El reporte redactado permanece fuera del repositorio.
- Trivy configuración: sin hallazgos altos/críticos pendientes después de excluir dependencias instaladas y justificar solo los servidores de test que necesitan root.
- Trivy imagen: sin vulnerabilidades altas/críticas corregibles en la imagen local analizada; repetir sobre el artefacto final.
- Recuperación en frío: respaldo y restauración reales de PostgreSQL y `/data` con datos sintéticos. Cuenta, vault, host key y credencial SFTP cifrada recuperados; bytes remotos verificados. Los scripts conservan permisos del usuario 65532. [Procedimiento](../guides/backup-restoration.md).
- HTTPS: instalación aislada sin puertos publicados; certificado de Caddy validado con su autoridad local. La prueba detectó y permitió corregir un panic al aceptar IPs sin CIDR como proxies.

## Gate restante

1. Cobertura completa final al 100 %, lint, contratos y módulo anidado.
2. Suite de navegador por protocolo y repetición de desconexión sin retries.
3. Imagen final escaneada, grafo inicial medido y benchmark comparable.
4. Actualización/rollback desde la revisión base con bóveda y transferencias anteriores.
5. Revisión de limpieza/retención: conservar transfers interrumpidos y no adoptar ni borrar temporales remotos sin propiedad comprobable.
6. Habilitar reporte privado de GitHub y verificar las protecciones del repositorio antes de publicar; requiere configuración externa.

## Actualización del ensayo de navegador

Primera ejecución sin retries: 156 pasan, tres fallan, 19 omitidas y dos no ejecutadas. Las causas reproducidas son: uso de `crypto.randomUUID` fuera de un contexto seguro; temporal oculto que FTP LIST no devuelve; y expectativa de reemplazo FTP que ya no es segura. Se reutilizó `lib/uuid`, se cambió el staging a `transferly-<UUID>.part` portable y se conserva el código de error del job de copia en el diálogo de sync. La nueva prueba de sync cubre SFTP con reemplazo y FTP con preservación del original. El candidato requiere la siguiente ejecución completa.

Retención: `scripts/prune-history.sql` probado en PostgreSQL aislado. Preview no elimina filas; aplicación elimina solo un upload completado, un download fallido y audit vencido; conserva uploads interrumpidos/fallidos/cancelados, copias fallidas y transfers activos. Cada operación está limitada a 1000 filas y 30 segundos.

La carga diferida tiene ahora un boundary compartido que conserva navegación y diálogos ante un import fallido, con mensaje traducido y recarga explícita; se añadió una regresión de navegador que bloquea el chunk y verifica recuperación. La revocación SSH también cierra la conexión durante la apertura, y la verificación periódica empieza antes del handshake del WebSocket.

## Validación del 9 de octubre

Frontend: 1.293 pruebas pasan, tipos y lint sin errores; cobertura de statements, branches, functions y lines al 100 %. Se conservó el umbral, sin retries. Las esperas bajo instrumentación se ajustaron a imports diferidos; las verificaciones de desconexión mantienen condiciones observables.

Las rutas remotas se validan como absolutas en el núcleo; los nombres virtuales dentro de ZIP usan su validación textual independiente. Los casos de conexión fallida y nombres inválidos no abren el archivo ni alteran datos. Copias de una sola conexión devuelven `copy_connection_limit` con guía específica.

Se están ejecutando las dos suites de navegador y veinte repeticiones de tabs de forma secuencial. La matriz se actualizará con sus resultados antes de considerar el candidato terminado.

Primera suite final de navegador (imagen con las correcciones completas): **163 pasan, 19 capturas opcionales de documentación omitidas, cero fallos y cero retries**. Go final pasa con `-race` y cobertura 100 %. El grafo inicial del build final ocupa **488.157 bytes gzip en 59 archivos**, dentro del presupuesto de 500.000. La comparación histórica completa mide 830.385 bytes; reducción aproximada del 41 %.

Segunda suite final: **163 pasan, 19 capturas opcionales omitidas, cero fallos y cero retries**. El cierre/conmutación/recuperación de pestañas pasa **20 repeticiones** sin retries. La prueba de carga diferida rechaza un chunk de settings, conserva el shell y recupera la sección con recarga. Trivy de la imagen final y configuración no encuentra altas/críticas corregibles; Gitleaks vuelve a revisar 614 commits sin filtraciones detectadas.

## Resultado local final

Ver [resultados completos del candidato](2026-10-09-validation-results.md). Las comparaciones alternadas en reposo combinan veinte descargas por versión: API **152,25 → 155,93 MiB/s** (+2,41 %); listados **3,715 → 3,615 ms** (−2,69 %). No se observa la regresión del 10 % en este escenario. CI remoto, configuración del repositorio y publicación siguen pendientes. Se completó después la extracción de descargas y movimiento del árbol en controladores; ver la validación final de esa entrega.

Cierre final de R14: controladores de descargas y movimiento extraídos; 1.293 pruebas, tipos/lint y cuatro métricas de cobertura al 100 %. Revalidación de 33 pruebas de navegador afectadas sin retries. Todos los gates locales del candidato están en verde; queda CI remoto/configuración/publicación sobre el commit definitivo.


## Extensión de compatibilidad SSH (2026-10-09)

Se conserva la corrección de group-exchange SHA-256 y se explicitan MAC y firmas seguros. Los errores de negociación ahora llegan como `ssh_algorithm_incompatible`/422 con categoría y algoritmos, sin reintentos automáticos. Se añadieron pruebas de servidores compatibles/incompatibles y firmas RSA seguras/obsoletas. No se habilitó criptografía obsoleta. Evidencia y limitaciones: [compatibilidad SSH](2026-10-09-ssh-compatibility.md).


### Timeout sin reconexión indefinida

Se corrigió la confusión entre errores de plazo/cancelación del conector y abandono real de la conexión compartida. Los timeouts TCP SFTP detienen los intentos automáticos y devuelven `remote_connection_timeout`/504 con guía para revisar acceso y reintentar manualmente. Regresiones del gestor, SFTP, HTTP y guía de errores incorporadas a la etapa del candidato.


## Gate completo posterior a SSH y timeout

Validación final del workspace: Go `-race` y cobertura oficial por función al 100 %; 1.296 pruebas frontend y cobertura al 100 %; tipos/lint y módulos pasan; regeneración sin diferencias; JavaScript inicial 489.273/500.000 bytes gzip; imagen nueva y todos los fixtures; 163 pruebas de navegador pasan sin retries y 19 capturas omitidas por configuración. Se completaron regresiones de firmantes restringidos, entradas hostiles acotadas, clave DSA rechazada antes de dial y diagnóstico ausente/desconocido. Notas de `1.0.0-rc.1` preparadas como borrador. No se cambió versión, commit, tag ni visibilidad; CI remoto y publicación siguen pendientes.
