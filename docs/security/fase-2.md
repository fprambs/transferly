# Revisión de seguridad: fase 2 (cerrada)

Fecha: 2026-09-26. Proceso: [README](README.md). Continúa el [informe de la fase 1](fase-1.md).

Alcance: adaptador FTP/FTPS (explícito e implícito, huella del certificado, juegos de caracteres, zona horaria del listado), planificador de transferencias (límites, prioridades, pausa), copia entre sitios y de carpetas, descarga de carpetas como ZIP, comprobación de sumas, reanudación en FTP, panel local (File System Access API), registro de actividad, caché de listados, importador del gestor de sitios de FileZilla y sus pantallas.

## Modelo de amenazas (STRIDE)

Activos y actores: los de la fase 1, más el disco del usuario (el panel local escribe en una carpeta que el navegador le concede) y los servidores FTP, que hablan un protocolo de texto sin autenticación de los datos.

Nuevos límites de confianza: servidor FTP contra Sentinel (respuestas PASV/EPSV, nombres en cualquier juego de caracteres); servidor remoto contra el disco del usuario (nombres que llegan a `saveToLocal` y al ZIP); archivo `sitemanager.xml` subido contra el analizador XML.

| Amenaza | Situación en la fase 2 |
|---|---|
| Suplantación | El certificado FTPS se fija por huella (TOFU) igual que una host key SSH, también en las conexiones de datos, que reanudan la sesión TLS de control. TLS 1.2 como mínimo y tope en 1.2 (ver riesgos aceptados). |
| Manipulación | Toda ruta pasa por `validPath` (sin CR, LF ni NUL) antes y después de convertirla al juego de caracteres del sitio; la copia con `verify` relee el destino y compara SHA-256; la reanudación en FTP comprueba el tamaño real antes de seguir. |
| Repudio | El registro de actividad guarda usuario, sitio, ruta, acción y código de error de cada operación, con retención acotada. |
| Fuga de información | El importador nunca devuelve contraseñas (solo el estado de cada sitio); el registro de actividad es privado por usuario; los adjuntos ZIP son inertes (`application/zip`, `attachment`, `nosniff`). |
| Denegación de servicio | Límites del planificador (global, por workspace, por usuario y por sitio); árboles limitados a profundidad 64 y 100 000 entradas (`tree_too_large`); importación limitada a 4 MiB, 2000 sitios y profundidad 12 sin DTD ni entidades; pool de 4 conexiones por sitio. |
| Elevación de privilegios | Matriz de autorización de los endpoints nuevos (viewer solo descarga, ZIP y suma; editor copia e importa; extraño recibe 404). Una copia entre sitios exige descarga en el origen y subida en el destino. |

## Ataques conocidos revisados

| Ataque | Aplica | Resultado | Verificación |
|---|---|---|---|
| FTP bounce (comando `PORT` hacia un tercero) | no | Sentinel no implementa el modo activo: nunca envía `PORT` ni `EPRT`, así que no puede ordenar al servidor que conecte a otro host. El modo activo queda descartado (ver decisión) | revisión del adaptador; el modo pasivo es el único |
| Secuestro por PASV (el servidor anuncia otra IP) | sí | La dirección anunciada se ignora: las conexiones de datos van siempre al host del control, y todas pasan por el `Dialer` de `netguard` | `TestDial_Destinations` (servidor que anuncia `192.0.2.10`) |
| Inyección de comandos por CRLF y NUL en rutas | sí | `validPath` rechaza los caracteres de control; tras convertir a un juego multibyte se vuelve a validar (`representable`), de modo que ningún carácter puede fabricar un CR, LF o NUL en el cable | `TestFS_HostileNames`; fuzz `FuzzRepresentable` (9 juegos de caracteres, 600 000 ejecuciones sin fallos) |
| Zip Slip y path traversal al escribir | sí | El ZIP usa nombres relativos limpios (`safeName`), sin `..`, barras invertidas ni rutas absolutas; en el panel local, `saveToLocal`, `makeLocalDirectory` y `openLocalDirectory` rechazan nombres vacíos, `.`, `..`, con separador o NUL (hallazgo 1) | `TestService_Archive`, `TestRemoteFiles_ArchiveHostileNames`, `refuses names and paths that would climb out of the folder` |
| Symlinks y dispositivos | sí | El ZIP y la copia de árboles omiten enlaces y dispositivos: nunca se sigue un enlace hacia fuera del árbol | `TestService_Archive` (`oddFS`), `TestService_CopyTree` |
| ZIP bombs, árboles gigantes y ciclos | sí | El recorrido tiene límites de profundidad y de número de entradas (`ErrTreeTooLarge`); el ZIP se genera en flujo, sin acumular en memoria | `TestService_CopyTree`, `TestService_Archive` (límites 3/100 y 5/4) |
| XXE y bombas de entidades en el importador | sí | El analizador rechaza DTD y entidades, limita tamaño, número de sitios y profundidad; cualquier entrada falla con un error del paquete o produce como máximo 2000 sitios con host | `TestParse_Refusals`, `TestParse_Limits`, fuzz `FuzzParse` (120 000 ejecuciones) |
| XSS por nombres en las pantallas nuevas | sí | Todo se renderiza como texto: registro de actividad, vista previa de la importación, diálogos de copia y dock | E2E `shows hostile names of the activity log as text`; el de la fase 1 sigue vigente |
| Sniffing de contenido en el ZIP | sí | Nombre de carpeta con comillas, `;`, HTML y no ASCII: el `Content-Disposition` sale como parámetro codificado y la respuesta lleva `nosniff` | `TestRemoteFiles_ArchiveHostileNames` |
| IDOR entre workspaces y usuarios en los endpoints nuevos | sí | Matriz de autorización de archivos, copias, importación, sumas, sondeo de zona horaria y actividad; la actividad y las transferencias son privadas por usuario | `TestPhaseTwoEndpoints_AuthorizationMatrix` |
| Fuga de contraseñas en la importación | sí | La respuesta solo lleva estado y motivo por sitio; las contraseñas se sellan en el vault y exigen que esté desbloqueado (`vault_locked` antes de escribir nada) | `TestImport_Filezilla`, `TestSecrets_NeverLeave` |
| Listado vacío bajo carga en FTP | sí (fiabilidad) | Se sospechó un listado vacío intermitente; no se reproduce con 8 sesiones concurrentes que mezclan listados y subidas contra vsftpd real | `TestVsftpd_ConcurrentSessions` (`-race -count=3`) |
| Degradación de TLS | sí | TLS 1.2 como mínimo, certificados fijados por huella; el cifrado en claro solo ocurre si el usuario elige FTP sin TLS | `TestDial_Certificates`, vsftpd explícito e implícito |

## Dependencias

| Herramienta | Objetivo | Resultado |
|---|---|---|
| `govulncheck ./...` | módulo Go | 0 vulnerabilidades alcanzables; sigue avisando de GO-2026-5932 (`x/crypto/openpgp`, sin corrección) en un módulo requerido, sin código que la llame |
| `pnpm audit` | frontend | sin vulnerabilidades conocidas |
| Trivy `fs` (vuln + misconfig, altas y críticas) | `go.mod`, `pnpm-lock.yaml`, tres Dockerfiles | 0 vulnerabilidades y 0 errores de configuración |

Librerías nuevas en la fase: `jlaffaye/ftp` v0.2.4 (sin avisos conocidos; se envuelve con validación de rutas propia, resolución de PASV descartada y `Dialer` de `netguard`) y `golang.org/x/text` (juegos de caracteres; sin avisos).

## Pruebas ofensivas realizadas

Contra servidores en proceso o el stack aislado (`compose.test.yaml`, con su propio SFTP y vsftpd; nunca contra los sitios del usuario): servidor FTP que anuncia una IP ajena en PASV; nombres con CR/LF/NUL, en nueve juegos de caracteres, generados por fuzz; `sitemanager.xml` con DTD, entidades, anidamiento profundo y truncados, generados por fuzz; árboles que superan los límites; carpetas con enlaces y dispositivos; nombres de carpeta con comillas, `;`, HTML y Unicode en el ZIP; nombres remotos con `..`, separadores y NUL hacia el disco local; llamadas anónimas, sin token, de extraños, de lectores, de editores y con sitios de otro workspace a cada endpoint nuevo; ocho sesiones FTP concurrentes mezclando listados y subidas. Los que dejaron una garantía útil son regresiones automáticas.

## Hallazgos

| # | Severidad | Hallazgo | Estado |
|---|---|---|---|
| 1 | Baja | El panel local escribía en el disco con los nombres que envía el servidor remoto sin validarlos en Sentinel; solo el navegador (File System Access API) impedía `..` o separadores | Corregido: `UnsafeLocalName` en `openLocalDirectory`, `makeLocalDirectory` y `saveToLocal`; regresión unitaria |
| 2 | Informativa | No había una matriz de autorización para los endpoints nuevos (copias, ZIP, sumas, sondeo de zona horaria, importación, actividad) | Corregido: `TestPhaseTwoEndpoints_AuthorizationMatrix`; sin fallos de fondo |
| 3 | Informativa | Faltaban pruebas de fuzz sobre las dos entradas hostiles nuevas (rutas convertidas de juego de caracteres e importador XML) | Corregido: `FuzzRepresentable` y `FuzzParse`, sin hallazgos |
| 4 | Informativa | El listado vacío intermitente visto en un E2E no se reproduce | Cerrado sin cambios de código; queda `TestVsftpd_ConcurrentSessions` como barrera |
| 5 | Baja | La suma de comprobación y el ZIP leen el archivo completo a través del servidor: un usuario autenticado puede gastar ancho de banda del servidor | Aceptado: el ZIP es una transferencia y cuenta para los límites del planificador; la suma se puede abandonar en cualquier momento y necesita permiso de descarga |
| 6 | Baja | El registro de actividad se filtra por usuario y workspace sin comprobar la pertenencia actual: un miembro retirado aún vería sus propias entradas antiguas | Aceptado: solo ve lo que hizo él mismo; la fase 3 lo revisa con los equipos |
| 7 | Informativa | FTPS queda limitado a TLS 1.2 por un fallo de vsftpd 3.0.3 con TLS 1.3 (cortes 426 en subidas grandes) | Aceptado: TLS 1.2 con las suites modernas de Go no es un debilitamiento; regresión de subida de 1 MiB |
| 8 | Informativa | El FTP sin TLS envía contraseña y datos en claro | Aceptado: es una elección explícita del usuario para servidores heredados; no es el valor por defecto |

## Decisión sobre el modo activo de FTP

Se descarta por ahora. Exige abrir puertos entrantes hacia Sentinel (incompatible con el despliegue en Docker de solo un puerto), obliga a enviar `PORT`/`EPRT` y reintroduce el rebote FTP y el robo de la conexión de datos. El modo pasivo con EPSV cubre los servidores actuales. Si un caso real lo exigiera, se reabre con estas condiciones: opción por sitio desactivada por defecto, rango de puertos configurable y explícito, aceptar la conexión solo desde la IP del servidor de control y pasar la dirección anunciada por `netguard`.

## Riesgos aceptados

Los hallazgos 5 a 8. Además: la seguridad del servidor FTP o SFTP remoto; que un usuario con permiso de descarga puede leer todo lo que ese sitio le deja ver; y que un administrador con acceso al proceso puede leer las llaves de las sesiones abiertas (como en la fase 1).

## Conclusión

No quedan hallazgos altos ni críticos abiertos. Las pruebas ofensivas dejaron una corrección menor (validación de nombres en el panel local) y tres barreras nuevas (matriz de autorización, y fuzz del convertidor de rutas y del importador). El FTP bounce no aplica al no existir el modo activo, y el PASV hostil se ignora con prueba. La fase 2 queda cerrada desde el punto de vista de seguridad; la fase 3 debe revisar la pertenencia a equipos, invitaciones, límites por miembro y la auditoría.
