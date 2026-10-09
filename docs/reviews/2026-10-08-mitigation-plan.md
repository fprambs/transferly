# Plan de mitigación para la versión estable

Base: [revisión del 8 de octubre de 2026](2026-10-08-stable-readiness.md), commit `ba2e4db`, frontend `0.9.0`.

Estado: **mitigaciones implementadas y validadas localmente; candidato y CI remoto pendientes**. El [seguimiento con evidencia](2026-10-08-mitigation-progress.md) distingue cambios probados y gates pendientes; la creación de este plan no acredita ningún cierre. Objetivo: cerrar los 16 hallazgos con evidencia y preparar un candidato estable reproducible.

## Estrategia y alcance

Mantener core/ports/adapters/transport, el contrato OpenAPI y el frontend por funcionalidades. Corregir las garantías compartidas dentro de esas capas; extraer componentes reutilizables cuando su responsabilidad quede definida. Evitar combinar una corrección de seguridad con una reorganización extensa de archivos.

La versión estable se planifica para una instancia de aplicación. Escalar a varias réplicas requiere otro diseño para estado, coordinación y eventos; los locks locales de este plan no se presentarán como coordinación distribuida.

Las reproducciones del informe afirman el defecto actual. Al convertirlas en regresiones, invertir sus expectativas: original conservado, acceso revocado, caché aislada, etc. Cada entrega debe incluir su corrección, prueba significativa, documentación afectada y evidencia de validación.

Aplicar la política de calidad de [ROADMAP.md](../ROADMAP.md): código/comentarios en inglés, i18n para mensajes, tiempos UTC, contratos regenerados cuando cambie el API y migraciones probadas cuando cambie persistencia. Conservar los umbrales actuales de cobertura. La revisión de reglas editoriales del informe será una tarea posterior y separada, no una excepción implícita al estándar existente.

## Etapas y entregas

| Etapa | Entregas propuestas | Resultado de salida |
| --- | --- | --- |
| 0. Verificación fiable | PR01: R06 | Local y CI ejecutan las mismas comprobaciones y servidores; cobertura Azure recuperada. |
| 1. Seguridad e integridad | PR02: R01; PR03: R03; PR04: R08; PR05–06: R02 | Acceso revocado, identidad aislada, rutas normalizadas y escrituras sin pérdida del original. |
| 2. Instalación y dependencias | PR07: R04; PR08: R05 | Instalación HTTPS documentada funcional y auditorías resueltas. |
| 3. Operaciones acotadas | PR09: R07; PR10: R09; PR11: R10; PR12: R11; PR13: R12 | Copias sin espera circular, previews actuales y límites efectivos sobre trabajo/bytes. |
| 4. UI y rendimiento | PR14: R15; PR15: R16; PR16: R14 | Fallos visibles, desconexión consistente y menor carga inicial. |
| 5. Candidato y publicación | PR17: R13 + tareas de operación | Imagen versionada del commit validado, restauración y actualización comprobadas. |

El orden indica dependencias y prioridad, no una obligación de agrupar todos los cambios de una etapa en un único PR. R04 y R05 pueden adelantarse después de PR01 si son correcciones pequeñas. Cada PR debe quedar utilizable y revisable por sí mismo.

## Mitigación de cada hallazgo

### R01 — Revocación efectiva de terminales · P1 · PR02

**Implementación.** Crear un registro de recursos interactivos por sesión con cancelación idempotente. Registrar la terminal antes de permitir intercambio de comandos y darla de baja al terminar. Conectar logout, invalidación de sesiones, deshabilitación de cuenta y cambios de permisos a esa revocación. Mantener una comprobación periódica con `Auth.Verify` como respaldo para expiración y cambios persistidos; no usar Authenticate para renovarla. Revalidar el rol del workspace y la habilitación de comandos.

No depender de cerrar todos los leases del pool: su comportamiento actual permite finalizar operaciones prestadas y cambiarlo globalmente afectaría transferencias. Cerrar específicamente WebSocket y shell, desbloquear lectores/escritores y liberar el lease una sola vez. La carrera entre revocación y apertura debe terminar con la terminal cerrada.

**Aceptación.** Después de completar un logout no se acepta otro comando. Una sesión vencida deja de operar dentro del intervalo de verificación configurado. El mismo resultado aplica a cuenta deshabilitada, eliminación/degradación de membresía y comandos desactivados. No quedan goroutines o leases retenidos ni una terminal de otra sesión se cierra por error.

**Validación.** Regresiones del router real, reloj controlado, dos sesiones del mismo usuario, apertura simultánea con logout y detector de carreras. E2E sobre SSH para cierre de la shell y pérdida de permisos.

### R02 — Escrituras seguras y staging propio · P1 · PR05–06

**PR05: contrato y commit.** Separar la capacidad de reanudar de la capacidad de reemplazar el destino. Definir un puerto opcional para commit/reemplazo y explicitar garantías por adaptador. Eliminar la secuencia destructiva `Remove(destination)` seguida de `Rename(part, destination)` como sustitución general. Evaluar soporte real de SFTP/FTP/WebDAV y la semántica de escrituras completas de cada object store mediante conformance tests.

**Decisión propuesta.** Usar reemplazo atómico donde esté soportado y comprobado. Si un servidor no ofrece una forma segura, devolver un error específico antes de destruir el original y ofrecer conservar ambos mediante otro nombre. No presentar backup + dos renames como atomicidad. Un fallback recuperable que mueva el original a backup exige journal persistente y recuperación tras crash; solo incorporarlo en otra entrega si es necesario para el alcance estable. Actualizar el editor para explicar capacidades y alternativas.

**PR06: staging y concurrencia.** Asociar cada temporal al ID de transferencia y verificar propietario, destino, modo y offset al reanudar. Serializar commits y escrituras incompatibles por destino normalizado. Definir limpieza de temporales huérfanos con antigüedad y referencia persistida; no borrar un `.part` ajeno por su nombre. Cubrir sitios guardados y conexiones rápidas. Documentar el límite de identidad si dos configuraciones distintas apuntan al mismo archivo o intervienen clientes externos.

**Compatibilidad.** No adoptar automáticamente un `<path>.part` anterior sin propiedad verificable. Detectar transferencias interrumpidas del formato previo y responder con un estado explicable; conservar su archivo remoto hasta una recuperación o limpieza deliberada. Si hace falta persistir staging/propiedad, añadir una migración incremental y actualizar consultas y contrato.

**Aceptación.** Un fallo de commit conserva los bytes originales. Dos subidas no comparten temporal ni mezclan bytes. Una reanudación ajena se rechaza antes de escribir. Un restart tiene un resultado definido y recuperable; no se promete exactamente una vez tras una respuesta de red ambigua. Las diferencias de atomicidad aparecen en capacidades y documentación.

**Validación.** Conformance por adaptador: create/overwrite/resume, dos usuarios, dos peticiones al mismo destino, rename rechazado, desconexión antes/después de commit y restart. Comprobar bytes con hashes y no solo existencia. Migraciones sobre base vacía y con transferencias anteriores.

**Dependencias.** R08 antes del lock por destino; primera entrega del contrato antes de la segunda. Es la tarea con mayor incertidumbre técnica del plan.

### R03 — Estado privado ligado a la sesión · P1 · PR03

**Implementación.** Unificar inicio, reemplazo, logout y expiración en un coordinador de sesión. Introducir una generación de sesión que cambie incluso cuando vuelve a entrar el mismo usuario. Cancelar peticiones, cerrar streams y limpiar QueryClient privado, Zustand, tabs persistidas, logs, prompts, fuentes de subida y handles locales. Mantener únicamente preferencias públicas elegidas explícitamente.

Invalidar primero la generación anterior; después limpiar. Las respuestas/callbacks tardíos deben comprobar su generación antes de publicar estado. Crear constructores de claves privadas compartidos para que Query y operaciones manuales usen la misma identidad. Observar cambios de sesión entre pestañas sin propagar tokens o datos privados por almacenamiento/mensajería. Revisar persistencia local para que una recarga no restaure estado del usuario anterior.

**Aceptación.** A → logout/expiración → B, sin recarga, no muestra ni reutiliza datos, archivos, prompts o permisos locales de A. Una petición de A que termina después del login de B no repuebla el estado. Repetir login con la misma cuenta tampoco recupera recursos de una generación revocada.

**Validación.** Vitest con promesas controladas y stores reales; E2E con dos cuentas y dos pestañas. Probar respuestas tardías HTTP, eventos WebSocket, lista de transferencias y raíz local.

### R04 — HTTPS coherente · P1 · PR07

**Implementación.** Corregir el ejemplo para declarar dominio y `TRANSFERLY_BASE_URL=https://...`. Elegir una fuente explícita de origen público y comprobar coherencia en el flujo de despliegue; preservar localhost HTTP de desarrollo. Añadir una configuración de confianza restringida para Caddy y comprobar IP real por la cadena de proxy. Documentar configuración de OAuth/WebAuthn y puerto/host esperados.

**Aceptación.** Una instalación limpia siguiendo README permite setup, login y cambios desde el dominio HTTPS. Cookie con Secure y HttpOnly; origen distinto rechazado. WebSocket y WebAuthn usan el origen esperado. Un cliente directo no puede falsificar su IP mediante X-Forwarded-For. Una configuración inconsistente falla con diagnóstico útil.

**Validación.** Tests de config/Compose y headers; integración de proxy en entorno local controlado con TLS. La validación no necesita publicar un dominio de producción ni emitir certificados públicos.

### R05 — Dependencias y auditoría · P1 · PR08

**Implementación.** Inventariar imports y uso de CLI; retirar herramientas no necesarias o ubicarlas en devDependencies. Actualizar de forma dirigida el árbol afectado y el lockfile, contrastando avisos vigentes y compatibilidad al implementar. Auditar runtime y build por separado sin ocultar el segundo.

**Aceptación.** Auditorías sin avisos altos/críticos pendientes para el candidato y sus herramientas. Si un aviso carece de parche, priorizar retirar/reemplazar la dependencia. Cualquier excepción inevitable debe identificar aviso, ruta, exposición, mitigación, revisión y vencimiento; no cuenta como cierre automático. La política actual del proyecto exige resolver altas/críticas, por lo que cambiar esa política requeriría una decisión explícita.

**Validación.** Instalación congelada desde cero, auditorías Go/npm, lint/tipos, build y tests de componentes afectados. Trivy sobre la imagen final y configuración. Verificar forks locales además del módulo original.

### R06 — Mismo gate en local y CI · P1 · PR01

**Implementación.** Extraer el arranque/ejecución del stack completo de pruebas a un script compartido por Makefile y CI. Activar todos los perfiles usados, esperar readiness, aislar datos y conservar reportes al fallar. Incorporar una regresión del escenario que falta en `azblobfs.copy` para recuperar su cobertura. No modificar umbrales ni convertir fallos en skips.

**Aceptación.** Un checkout limpio ejecuta los mismos protocolos en local y GitHub. Un servidor ausente o una prueba fallida hacen fallar el job. Go conserva detector de carreras; código generado queda sin diferencias. Reportes y trazas disponibles para diagnóstico.

**Validación.** Comando único desde stack vacío; ejecución real en CI del commit propuesto. Conectar las pruebas de integración externas a sus variables `TRANSFERLY_TEST_*`, o separarlas en un job explícito que no las omita silenciosamente. Los 19 tests de generación de capturas mantienen su omisión deliberada fuera de ese uso.

### R07 — Copias sin espera circular · P2 · PR09

**Implementación.** Planificar las conexiones de ambos extremos antes de comenzar streaming. Para límites de una conexión: usar una copia remota comprobada cuando exista; en otro caso rechazar anticipadamente con un error específico. No introducir buffering ilimitado ni un timeout como sustituto de resolver la espera circular. Reservar/adquirir ambos recursos con orden y liberación comunes, sin mantener uno mientras se espera indefinidamente el otro.

**Aceptación.** Copia grande dentro de un sitio con una conexión completa por una vía soportada o se rechaza antes de mover bytes. Copias A → B y B → A concurrentes no se bloquean. Cancelación, fallos y shutdown liberan ambas reservas. El scheduler respeta límites cuando una copia consume dos conexiones.

**Validación.** Pool real con límites 1/2/4, archivos mayores al buffering, copias simultáneas, cancelación y errores de ambos extremos. No basar la prueba únicamente en que venza un deadline.

**Dependencias.** R08 y contrato revisado de archivos de R02.

### R08 — Invariantes sobre rutas normalizadas · P2 · PR04

**Implementación.** Centralizar validación/normalización de rutas absolutas del núcleo antes de comparación y uso. Normalizar origen y destino; rechazar igualdad y descendencia del mismo recurso. Aplicar la misma regla en copia, rename, staging y extracción donde corresponda. Revisar identidad de sitios equivalentes y symlinks conforme a capacidades del adaptador; no considerar `path.Clean` una resolución física.

**Aceptación.** `/src/../src` → `/src/child` y variantes equivalentes se rechazan antes de crear carpetas o temporales. Las rutas válidas, Unicode y nombres especiales siguen funcionando. Las restricciones físicas no demostrables tienen una limitación explícita.

**Validación.** Tests de tabla y fuzzing acotado de rutas: repetición de separadores, `.`/`..`, raíz, controles, alias y symlinks cuando el servidor permita resolverlos.

### R09 — Identidad e invalidación de previews · P2 · PR10

**Implementación.** Extender los constructores de claves de R03 con versión del archivo y generación de raíz local. Incorporar invalidación por upload/save/rename/delete. Usar revisión/ETag cuando el adaptador lo ofrezca; tamaño y mtime son una aproximación que no detecta cambios del mismo tamaño dentro de la precisión del timestamp. En ese caso revalidar al abrir o limitar el tiempo de frescura. Aplicar a head, whole-file, hex, adjuntos y archivos anidados.

**Aceptación.** Reabrir después de guardar muestra los bytes nuevos; dos raíces locales con la misma ruta no comparten datos. Una edición externa del mismo tamaño no queda oculta indefinidamente. El presupuesto de memoria contempla la suma de previews retenidos, no solo cada archivo.

**Validación.** QueryClient real, cambio de raíz, timestamps de precisión limitada y cambios externos. E2E abrir → editar/subir → volver a abrir sin recarga.

**Dependencias.** R03; coordinar con lectores acotados de R11.

### R10 — Walker reutilizable con límites · P2 · PR11

**Implementación.** Crear un recorrido común del frontend con máximo de todas las entradas, profundidad, rutas visitadas, presupuesto temporal y AbortSignal propagado a cada listado. Validar cada hijo antes de añadirlo a la cola; sustituir `shift()` por índice. Adaptar download, sync, search y uploads a esa política, conservando sus límites de resultados particulares.

Aplicar cuotas durante cada batch, antes de acumular el árbol completo. `treeFromInput` debe pasar por los mismos límites. La cancelación de UI aborta también el request activo. Diferenciar cancelación de árbol demasiado grande mediante códigos traducibles.

**Aceptación.** Árboles de carpetas vacías, rutas repetidas y profundidad extrema terminan por límite o cancelación. No se solicitan nuevos listados tras cancelar. Los árboles admitidos conservan orden y contenido esperado; symlinks se manejan conforme a la política explícita.

**Validación.** Generadores de árboles y batches; árbol ancho y profundo; cancelación durante listado; pruebas de sync/download/upload usando el walker real.

### R11 — Límites sobre bytes recibidos y duración · P2 · PR12

**Implementación.** Reutilizar/ampliar `readCapped` para el editor con límite de 2 MiB de bytes reales y AbortSignal al cerrar. Propagar la señal por Api/Transport y liberar streams ante error. Validar la nueva metadata, sin confiar en ella como único límite.

En backend, introducir lectores limitados para errores remotos y decoders de metadatos, máximo acumulado de listings y detección de tokens de página repetidos. Añadir deadlines por operación corta. Valores iniciales propuestos para contrastar con fixtures reales: error remoto 64 KiB; metadatos por respuesta 8 MiB; 100.000 entradas acumuladas; operaciones cortas 30 s. Configurar/exponer excepciones necesarias de forma controlada. Transfers largas mantienen streaming y cancelación propios.

**Aceptación.** Un servidor que declara un tamaño pequeño y envía más bytes es detenido sin leer el resto completo. Bodies interminables de error/metadatos terminan; listings no acumulan páginas indefinidas. Cerrar el editor aborta su descarga. Archivos grandes de transferencia siguen funcionando.

**Validación.** Servidores de prueba con tamaño falso, chunked ilimitado, cuerpo lento, XML/JSON grande y paginación repetida. Verificar bytes consumidos y cierre de body, no provocar agotamiento real del proceso.

### R12 — Parsing y descompresión con presupuesto · P2 · PR13

**Implementación.** Prevalidar índices ZIP/ZIP64: tamaños, offsets, rangos y cantidad de entradas antes de materializarlos. Revisar presupuestos de memoria de headers 7z y otros parsers. Para Office, limitar cantidad de partes, bytes por parte, bytes descomprimidos totales y recursos gráficos antes de entregarlos a JSZip/docx-preview/SheetJS. Centralizar límites en una política de preview.

Usar ejecución cancelable/terminable para decoders costosos cuando las librerías y CSP lo permitan. Un timeout en la misma hebra no puede detener parsing síncrono bloqueante: comprobar la interrupción real. Si un formato no permite defender el presupuesto, rechazar su preview y ofrecer descarga para el candidato estable.

**Aceptación.** Un archivo comprimido pequeño con índice/partes desproporcionados se rechaza antes de una asignación o descompresión excesiva. El cliente conserva capacidad de cerrar el visor. El sandbox mantiene sus restricciones de origen y red.

**Validación.** Fixtures adversarias pequeñas y pruebas instrumentadas de límites, integer overflow/offsets y descompresión; fuzzing acotado de parsers. Medir memoria y tiempos de muestras normales para ajustar presupuestos y documentar formatos rechazados.

**Dependencias.** R11 aporta lectores/presupuestos. R12 se valida antes de optimizar su carga bajo demanda en R14.

### R13 — Release reproducible y versionada · P2 · PR17

**Implementación.** Workflow para tags `vX.Y.Z`, pasando VERSION al Docker build; validar que versión de frontend, binario, API y labels coincidan. Reutilizar los gates del commit exacto y publicar etiqueta SemVer protegida de sobrescritura más digest. Separar imágenes de main de la etiqueta estable. Alinear script de release, README y guía con GHCR.

**Aceptación.** Desde un tag, el candidato reporta esa versión en todos sus puntos; actualización y rollback pueden identificar el digest exacto. No se publica una imagen stable con VERSION=dev ni se reemplaza un tag existente. Un gate fallido impide publicación.

**Validación.** Build local de candidato y comprobación automatizada de versiones; ensayo del workflow sin publicar la estable. Para publicar finalmente, revisar el candidato concreto, sus reportes y notas; el pedido actual autoriza planificación, no publicación externa.

**Dependencias.** Todos los bloqueantes cerrados y tareas de operación completadas.

### R14 — Menor carga inicial y responsabilidades claras · P2 · PR16

**Implementación.** Extraer hooks/controladores de operaciones de Explorer y Sidebar en entregas funcionales pequeñas. Cargar bajo demanda editor, terminal, configuración y visores costosos con estados de carga/error. Medir el grafo inicial completo y requests de login, explorador y primera apertura de cada feature, no únicamente el archivo main.

**Aceptación propuesta.** Reducir al menos 30 % el JavaScript inicial gzip respecto de la base medida bajo el mismo escenario; editor/terminal no se descargan antes de su uso. Establecer el presupuesto definitivo después de la primera medición y automatizarlo. Aumento máximo de 10 % en benchmarks comparables de throughput/listados, conforme al roadmap. No ampliar el umbral de advertencia como solución.

**Validación.** Build con análisis de chunks y prueba de requests iniciales. E2E de features cargadas bajo demanda, errores de carga y navegación. Benchmarks reproducibles con configuración y resultados guardados; revisar parse/evaluación además del peso de red.

**Dependencias.** R09–R12 y R16 cerrados para no mezclar problemas de comportamiento con división de bundles.

### R15 — Fallos visibles y persistencia diagnosticable · P3 · PR14

**Implementación.** Política común de errores: cancelación esperada, error accionable en UI y fallo operativo. Aplicarla a cancel/pause/resume/remove y reload del editor. Mantener códigos del API e i18n; aborts esperados no generan avisos engañosos.

Reemplazar descarte silencioso en tracker por registro y una política de persistencia diferenciada: progreso recuperable/coalescido frente a transición final. Definir retries acotados y reconciliación; no reintentar comandos remotos ambiguos como si fueran idempotentes. Publicar estado consistente y hacer diagnosticable una divergencia del store. Auditoría best effort registra su propio fallo sin filtrar secretos.

**Aceptación.** El usuario sabe cuando cancelar o recargar falló. Un fallo de DB deja evidencia y una recuperación definida; una transición final no se anuncia persistentemente guardada si no lo está. No hay promesas rechazadas sin tratamiento, loops de retries ni credenciales en logs.

**Validación.** Inyección de errores de API/DB, recuperación y reinicio; respuestas tardías y aborts. Tests de mensajes/códigos y redacción de datos sensibles.

### R16 — Desconexión consistente al cerrar pestañas · P2 · PR15

**Investigación.** Conservar traza del fallo y reproducir con latencia y listados/eventos en vuelo. Determinar si se trata de reapertura, respuesta obsoleta o estado visual; las tres repeticiones aisladas del informe no prueban una causa.

**Implementación condicionada a evidencia.** Unificar el flujo cancelar requests → impedir reapertura desde la vista cerrada → disconnect → reconciliar estado. Esperar/gestionar el resultado y ordenar respuestas HTTP/eventos para que un snapshot anterior no restaure Connected. Compartir el controlador con la desconexión manual; conservar otros tabs que sí usan el sitio y operaciones que deben finalizar según su política.

**Aceptación.** Después de cerrar la última pestaña de un sitio, la conexión se cierra y el indicador converge al estado real; una pestaña restante no pierde su conexión. Fallos se muestran y permiten recuperación. Sin sleeps fijos ni aumento de retries para ocultar el problema.

**Validación.** Test determinista de la carrera encontrada, 20 repeticiones E2E sin retries con latencia controlada y al menos dos ejecuciones de la suite completa sin ese flaky.

**Dependencias.** R03 y política de errores de R15; coordinar con cancelación de R11.

## Tareas adicionales de publicación y operación

Estas tareas cierran aspectos del informe que no tienen un ID R propio. Son parte del gate final:

- [x] Escanear secretos en todo el historial; revisar resultados redactados. Si hay credenciales reales, revocarlas/rotarlas antes de hacer público el repositorio; cualquier reescritura de historial se planifica por separado.
- [x] Añadir SECURITY.md con reporte privado y política de versiones soportadas; verificar licencias/procedencia y mantenimiento de forks locales.
- [x] Excluir `.env.*`, cobertura y artefactos locales del contexto Docker; comprobar el contenido del contexto y capas sin imprimir secretos.
- [x] Documentar y ensayar backup/restauración de PostgreSQL y `/data`, incluida clave de instancia. Comprobar acceso, vault y lectura de credenciales después de restaurar.
- [x] Ensayar actualización desde 0.9.0 con datos y transferencias interrumpidas; definir rollback compatible. Una migración destructiva no se considera revertible solo por conservar la imagen anterior.
- [x] Definir retención/paginación del historial de transferencias y audit; limpiar solo estados terminales elegibles y conservar lo necesario para reanudar/recuperar.
- [x] Ejecutar Trivy de imagen/configuración, integración real por protocolo y benchmarks de carga/latencia acotados; registrar versiones, recursos y resultados.

## Seguimiento y definición de terminado

Estado por hallazgo: pendiente → en implementación → en validación → cerrado. Un workaround puede figurar como mitigado con limitación documentada, pero no se etiqueta cerrado si la garantía ofrecida sigue incumplida. Cada cierre enlaza commit/PR, pruebas, resultado de CI y riesgo residual.

| ID | Entrega | Dependencias | Estado local al 9 de octubre |
| --- | --- | --- | --- |
| R01 | PR02 | PR01 para validación fiable | Validado local |
| R02 | PR05–06 | R08; decisión por adaptador; compatibilidad de staging | Validado local |
| R03 | PR03 | PR01 | Validado local |
| R04 | PR07 | PR01 | Validado local |
| R05 | PR08 | PR01; avisos vigentes al implementar | Validado local |
| R06 | PR01 | Ninguna | Validado local |
| R07 | PR09 | R08 y contrato R02 | Validado local |
| R08 | PR04 | Ninguna funcional; PR01 para gate | Validado local |
| R09 | PR10 | R03 | Validado local |
| R10 | PR11 | Ninguna funcional | Validado local |
| R11 | PR12 | Coordinación con R03/R09/R10 | Validado local |
| R12 | PR13 | R11 | Validado local |
| R13 | PR17 | Gate final completo | RC publicado; público/estable pendientes; CI diferido |
| R14 | PR16 | R09–R12, R16 | Validado local; controladores extraídos |
| R15 | PR14 | Ninguna funcional; integrar con R03/R11 | Validado local |
| R16 | PR15 | R03, R15; coordinación con R11 | Validado local |

Para cada PR: pruebas específicas primero; lint/tipos/contrato de capas afectadas; conformidad cuando cambie un adaptador; suite completa y seguridad en CI. Evitar ejecutar la batería completa repetidamente sin cambios o fallos que lo justifiquen.

Para declarar estable: todos los P1 cerrados; cada P2 cerrado o funcionalidad limitada/deshabilitada de forma explícita y comprobada; R15 incluido por su impacto en diagnóstico y recuperación; gate del commit exacto aprobado (local para este candidato, según la excepción acordada abajo); instalación HTTPS, actualización/restauración y presupuestos comprobados; versión/digest identificables. Preparar primero un candidato `1.0.0-rc.1` con notas y evidencia, antes del lanzamiento final.

**Primera ejecución propuesta:** PR01 para recuperar el gate; después PR02 y PR03 de seguridad/identidad, PR04 de rutas y PR05–06 de integridad. La estimación de calendario se hará tras diseñar las garantías de escritura de R02 y contrastar los parsers de R12: ambos concentran la mayor incertidumbre, por lo que no se fija una fecha de release antes de resolverlos.

## Resultado local

La [validación final del 9 de octubre](2026-10-09-validation-results.md) registra cobertura, dos suites de navegador, veinte repeticiones, seguridad, restauración/actualización y comparación de rendimiento. La publicación sigue pendiente de CI sobre el commit definitivo y configuración del repositorio. Los límites de una instancia, reemplazo FTP y recuperación de temporales están documentados.

- [x] Aceptar el gate local del candidato; CI remoto diferido por decisión del usuario.
- [x] Construir y probar la imagen versionada del candidato; hashes registrados.
- [x] Integrar el candidato actualizado en main y publicar su imagen/version RC.
- [ ] Verificar reporte privado/protecciones cuando el repositorio sea público y aprobar la publicación estable.


## Revalidación del candidato con SSH y timeout

El gate local completo vuelve a pasar después de las extensiones SSH: Go con detector de carreras y cobertura por función al 100 %, 1.296 pruebas frontend al 100 %, tipos/lint, módulos y contratos generados, build de 489.273 bytes gzip y 163 pruebas de navegador aprobadas sin retries (19 capturas omitidas deliberadamente). Véase la [evidencia actualizada](2026-10-09-validation-results.md) y el [borrador de notas de 1.0.0-rc.1](../releases/1.0.0-rc.1.md).

R13 continúa pendiente: el workspace todavía informa versión 0.9.0/dev. Preparar la identidad del candidato y su commit definitivo, ejecutar CI remoto y verificar protecciones/reporte privado precede al tag y publicación.


## Identidad del candidato

Versión preparada: `1.0.0-rc.1`; rama de validación: `release/1.0.0-rc.1`. La configuración CI ejecuta los gates sobre ramas `release/**` sin publicar imágenes. Las notas están en CHANGELOG.md y docs/releases/1.0.0-rc.1.md. El script de release admite prereleases y rechaza identificadores numéricos con ceros iniciales. Tag, imagen de release, CI remoto aprobado y verificación de protecciones/reporte privado continúan pendientes.


## Decisión vigente: gate local y CI diferido

El 9 de octubre el usuario acepta usar la validación local de Docker para este candidato y diferir CI remoto, sin contratar un plan de pago. El candidato es `1.0.0-rc.1`, rama `release/1.0.0-rc.1`, commit de código `cfe9a8b81551a82a73a039cd2ebb0bd1c219cc6c`. La ejecución remota [37974611676](https://github.com/fprambs/transferly/actions/runs/37974611676) no inició ninguno de los cuatro jobs de comprobación por bloqueo de facturación/límite de gasto; no equivale a pruebas fallidas ni aprobadas.

Se conservan workflow, pruebas, umbrales y escáneres. La excepción es el lugar de ejecución del gate, no una reducción de controles. Go con detector de carreras y 100 % por función, frontend con 1.296 pruebas y 100 %, lint/tipos, instalación congelada y build de 489.273 bytes gzip pasaron en la preparación de ese commit. El navegador completo pasó sobre la imagen de prueba previa a cambiar únicamente la identidad del candidato: 163 aprobadas, 19 capturas deliberadamente omitidas, sin retries. Esa imagen informa `dev`; falta construir/probar/publicar la imagen con identidad de release y registrar su digest.

GitHub confirma repositorio privado y `main` sin protección. La API de rulesets devuelve 403 por disponibilidad del plan y el endpoint de reporte privado devuelve 404; ese último control no está verificado. Antes del lanzamiento público hay que comprobar/configurar estos controles en el estado público del repositorio. CI remoto puede retomarse entonces o mediante un runner propio, como tarea separada.

Las etapas 0–4 están validadas localmente. R13 sigue abierto únicamente por entrega y publicación: imagen/version/digest, prueba del candidato distribuible, integración en main, configuración del repositorio público y lanzamiento estable. No se creó tag, release ni se cambió visibilidad.


## Imagen RC validada

Imagen local `transferly:1.0.0-rc.1`, commit de código `75c39f1`, Go 1.27.2. Binario y API reportan `v1.0.0-rc.1`; exportación y carga verificadas. Digest del índice OCI local: `sha256:fcf224a3c6f1fc0525ca014a4268f3bccd16bd1277ed3859669190001bffa3f8`. Archivo y checksums locales: `/tmp/transferly-release/`; [metadatos completos](../releases/1.0.0-rc.1-image.json).

Se corrigieron dos avisos HIGH de la primera imagen actualizando Go. Go completo con `-race` y cobertura por función al 100 %, govulncheck sin uso afectado, Trivy con cero HIGH/CRITICAL (incluidos sin parche), y 163 pruebas de navegador sin retries. El commit de seguridad es local y no fue enviado; no se publicó imagen, tag ni release y no se cambiaron llaves de Git ni visibilidad. R13 pendiente: integrar/publicar candidato, verificar controles del repositorio público y lanzar estable.


## Integración y preparación de publicación

La integración del candidato en main conserva el historial mediante fast-forward. El código validado es `75c39f1`; los commits posteriores de documentación no cambian la imagen ya probada. La [lista concreta de publicación](../releases/1.0.0-rc.1-publication.md) identifica notas, versión, hashes, artefacto y pasos pendientes. CI remoto sigue diferido; tag RC, subida al registro, prerelease y visibilidad pública no se ejecutan como parte de esta preparación.


## RC entregado

El 9 de octubre se publicó [v1.0.0-rc.1](https://github.com/fprambs/transferly/releases/tag/v1.0.0-rc.1) como prerelease, con archivo de imagen, checksums y metadatos. Main contiene el candidato y el tag apunta a `0a9a231`. GHCR: `ghcr.io/fprambs/transferly:v1.0.0-rc.1`, digest `sha256:fcf224a3c6f1fc0525ca014a4268f3bccd16bd1277ed3859669190001bffa3f8`; coincide con el índice validado localmente y se verificó pull/run remoto.

Repositorio y paquete permanecen privados; no se cambió facturación, protecciones ni llaves SSH. R13 aún requiere publicación pública/configuración de acceso y seguridad, comprobación del candidato distribuido y promoción a estable 1.0.0. CI sigue diferido por la decisión vigente. Esta entrada actualiza los estados de preparación anteriores, sin presentar CI como aprobado.
