# Revisión de seguridad: fase 3 (cerrada)

Fecha: 2026-09-27. Proceso: [README](README.md). Continúa el [informe de la fase 2](fase-2.md).

Alcance: workspaces de equipo y roles (3.1, 3.4), invitaciones de un solo uso (3.2), claves de workspace y su rotación (3.3), registro de auditoría de equipos (3.5), segundo factor con app de autenticación, passkeys y códigos de recuperación (3.6), administración de la instancia con red permitida editable (3.7) y límites de ancho de banda y cuotas (3.8). Decisiones en [ADR-0006](../adr/0006-teams-and-invitations.md) y [ADR-0007](../adr/0007-second-factor.md).

## Modelo de amenazas (STRIDE)

Activos nuevos: la pertenencia a un equipo y el rol (deciden qué se ve y qué se cambia), la clave de cada workspace y sus copias selladas por miembro, los tokens de invitación, los secretos del segundo factor (semilla TOTP sellada, credenciales WebAuthn, hashes de códigos de recuperación), el registro de auditoría y la configuración de la instancia (redes permitidas, límites).

Actores nuevos: miembro con cada rol (viewer, editor, admin, owner), miembro retirado, invitado con un enlace, administrador de la instancia (distinto del rol de un workspace), atacante que conoce la contraseña de una cuenta con segundo factor, y atacante que ve un enlace de invitación.

Límites de confianza nuevos: miembro contra miembro de otro equipo; enlace de invitación contra cuenta; contraseña contra segundo factor; administrador de la instancia contra la red interna del servidor (redes permitidas).

| Amenaza | Situación en la fase 3 |
|---|---|
| Suplantación | Un enlace de invitación sirve una vez, para un solo correo y 7 días como máximo; se guarda solo su hash SHA-256 y el token viaja en el fragmento de la URL (no llega a logs ni a `Referer`). El segundo factor exige TOTP, passkey o un código de recuperación de un solo uso tras la contraseña; el desafío es un HMAC de 5 minutos que solo se emite con contraseña correcta. |
| Manipulación | El rol de un miembro solo cambia por quien tiene un rango superior y hacia un rango inferior al propio; el propietario no se toca (`owner_bound`). Las claves de workspace van selladas por miembro con X25519 y HKDF; quitar a alguien rota la clave y vuelve a sellar los secretos. Las redes permitidas y los límites se validan y se aplican bajo transacción. |
| Repudio | Cada cambio de miembros, invitaciones, sitios, claves y servidores de confianza queda en `audit_events` con actor, objetivo, dirección IP y metadatos sin secretos; las acciones de administración (`admin.*`) van a la auditoría de la instancia. |
| Fuga de información | Un extraño recibe 404 idéntico al de un workspace inexistente; una invitación revocada, usada, caducada o inventada muestra la misma respuesta; el inicio de sesión no distingue cuenta inexistente de contraseña errónea (ni en el tiempo); solo se revela que una cuenta está deshabilitada tras acertar la contraseña. Las respuestas de administración nunca llevan hashes ni secretos. |
| Denegación de servicio | Límites por IP y por cuenta en inicio de sesión, segundo factor, passkeys e invitaciones; máximo de 50 invitaciones pendientes por workspace; cuotas de sitios, equipos, miembros y volumen diario; topes de ancho de banda y de transferencias simultáneas; máximo de passkeys por cuenta. |
| Elevación de privilegios | Matriz de autorización de miembros, invitaciones y auditoría (con extraños, lectores, editores, admins y propietario); las operaciones de administración comprueban el rol de la instancia en el servicio, no solo en la ruta; nadie cambia su propia cuenta desde el panel y el último administrador activo no se puede deshabilitar ni degradar, ni siquiera con dos cambios simultáneos (hallazgo 1). |

## Ataques conocidos revisados

| Ataque | Aplica | Resultado | Verificación |
|---|---|---|---|
| IDOR y acceso entre workspaces | sí | Todo acceso pasa por `Authorize(usuario, workspace, acción)`; un extraño recibe 404 en miembros, invitaciones, auditoría y creación de invitaciones de otro equipo. Los identificadores de una invitación o un miembro de otro equipo, enviados por el equipo propio, dan 404 y no cambian nada | `TestPhaseThree_CrossWorkspaceAndEscalation`, `TestTeams_AuthorizationMatrix`, `TestInvitations_AuthorizationAndLimits` |
| Escalada de rol | sí | Un admin no invita como admin ni entrega `owner` (`invalid_role`), no toca al propietario (`owner_bound`) ni transfiere el workspace; un editor no se sube a admin. Un miembro degradado pierde lo que permitía el rol viejo en la siguiente petición y uno retirado pierde el workspace | `TestPhaseThree_CrossWorkspaceAndEscalation`, `TestTeams_Lifecycle` |
| Reutilización y adivinación de tokens de invitación | sí | Token de 256 bits, hash SHA-256 en base de datos, un solo uso, 7 días, atado a un correo; reinvitar reemplaza el anterior; respuesta uniforme para cualquier fallo y límite de intentos por IP | `TestInvitations_Lifecycle`, `RunInvite` (aceptar consume y crea la membresía en un paso) |
| Toma de una cuenta existente con una invitación | sí | `join` crea solo la cuenta del correo invitado; si ya existe responde `account_exists` sin abrir sesión, y `accept` exige la sesión de esa cuenta | `TestInvitations_Lifecycle` |
| Enumeración de usuarios | sí | Login uniforme en error y tiempo (hash de relleno); invitación inexistente, usada o de otro correo dan el mismo 404; no hay registro abierto. Un administrador sí ve todas las cuentas por diseño | `TestLogin` (auth), `TestCheckPasswordAndVerify`, `TestInvitations_AuthorizationAndLimits`, `TestAdmin_Refusals` |
| Bypass del segundo factor | sí | Ningún camino abre sesión sin el segundo paso cuando la cuenta lo tiene: el inicio de sesión responde 202 con un desafío; `join` solo crea cuentas nuevas; `accept` requiere sesión; el desafío caduca a los 5 minutos. Un código TOTP no se acepta dos veces (paso monótono) y un código de recuperación se consume atómicamente | `TestMFA_Lifecycle`, `TestMFA_Protection`, `TestPasskeys_Lifecycle`, `TestService_VerifyCode`, `TestService_Recovery`, `RunFactor` (`AdvanceStep` condicional) |
| Fuerza bruta del segundo factor | sí | Límite de 5 intentos por IP y por cuenta cada 30 s, compartido entre TOTP, recuperación y passkeys, y otro para confirmar con contraseña | `TestMFA_Protection`, `TestPasskeys_Protection` |
| Desactivar el segundo factor con una sesión robada | sí | Desactivarlo, regenerar los códigos y quitar una passkey piden la contraseña otra vez, con límite propio | `TestMFA_Protection`, `TestPasskeys_Protection`, `TestPasskeys_RemoveFailure` |
| Sustitución de passkey y RP ID | sí | El RP ID sale del host del origen configurado y los orígenes se validan; cada ceremonia se ata al usuario y se consume una vez; el contador de firmas avanza | `TestService_Registration`, `TestService_Login`, `TestPasskeys_Lifecycle`, e2e con autenticador virtual |
| Fuga de la semilla TOTP | sí | Se guarda sellada con AES-GCM bajo una clave derivada por HKDF de la clave de instancia, ligada al usuario; la API la devuelve una sola vez, al configurarla | `TestService_Enrolment` de `mfa` |
| Fuga de claves de workspace y rotación | sí | La clave de un workspace solo existe sellada por miembro (X25519 + HKDF-SHA256 + AES-GCM); quitar a un miembro marca la rotación y el siguiente miembro con la clave rota y vuelve a sellar los secretos; quien salió no puede abrir la versión nueva | `TestTeams_KeyRotation`, `RunKeys`, `TestTeams_SharedKeys` |
| Carrera al retirar al último administrador | sí | Se corrigió (hallazgo 1): la comprobación y el cambio corren bajo un bloqueo asesor de Postgres | `TestAdminStore_TheLastAdministratorSurvivesRaces` |
| SSRF por las redes permitidas | sí | El `Dialer` de `netguard` valida la IP resuelta al conectar (resiste DNS rebinding); desde el panel no se puede abrir loopback, `0.0.0.0/8`, link-local y metadatos de nube, multicast, reservados ni redes más anchas que /8 o /7 (hallazgo 2) | `TestParseNetworks`, `TestPolicy_Extra`, `TestAdmin_Networks` |
| Denegación de servicio por un tope de ancho de banda mal puesto | sí | Un tope de 1 B/s dejaría una petición esperando horas; se rechazan topes por debajo de 1 KiB/s (hallazgo 3). La espera se cancela con el contexto de la petición | `TestService_Set`, `TestSleep` |
| Elusión de cuotas | sí | Las cuotas se preguntan en el servicio que crea el recurso (sitios, invitaciones, transferencias) y en el manejador de equipos, también para la importación de FileZilla, que crea sitios por el mismo servicio | `TestLimits_Quotas`, `TestService_Quota` |
| CSRF en la administración | sí | Todas las operaciones de administración y de límites exigen sesión y token CSRF por el contrato OpenAPI | `TestAdmin_Refusals` |
| Inyección SQL | sí | Todas las consultas nuevas usan sqlc con parámetros; el filtro de acciones de auditoría se valida con una expresión regular y va parametrizado | revisión y `golangci-lint` |
| XSS por correos y nombres en las pantallas nuevas | sí | Todo se pinta como texto (miembros, invitaciones, cuentas, auditoría); el enlace de invitación se muestra en un campo de solo lectura | E2E de equipos y de administración, con las pruebas de accesibilidad |

## Dependencias

| Herramienta | Objetivo | Resultado |
|---|---|---|
| `govulncheck ./...` | módulo Go | 0 vulnerabilidades alcanzables; el aviso de un módulo requerido sin código que lo llame es el mismo de la fase 2 |
| `pnpm audit` | frontend | sin vulnerabilidades conocidas |
| Trivy `fs` (vuln + misconfig, altas y críticas) | `go.mod`, `pnpm-lock.yaml`, tres Dockerfiles | 0 vulnerabilidades y 0 errores de configuración |

Librerías nuevas en la fase: `go-webauthn/webauthn` v0.18.2 (ceremonias WebAuthn; sin avisos conocidos, guía en [second-factor](../guides/second-factor.md)) y `qrcode` en el frontend (solo dibuja la URI en un `<img>` con un data URL, guía en [qr-codes](../guides/qr-codes.md)). TOTP, X25519, HKDF y AES-GCM salen de la biblioteca estándar y de `x/crypto`.

## Pruebas ofensivas realizadas

Contra el servidor en proceso y contra el stack aislado (`compose.test.yaml`, nunca contra los sitios del usuario): llamadas anónimas, sin token CSRF, de extraños, de lectores, de editores, de admins y del propietario a cada endpoint de miembros, invitaciones, auditoría, administración y límites; identificadores de otro equipo enviados por el propio; intentos de escalar de rol por invitación y por cambio de rol; sesiones de miembros degradados y retirados; reutilización de invitaciones, de códigos TOTP y de códigos de recuperación; desafíos vencidos; ráfagas contra los límites de intentos; quince rondas de dos administradores que se retiran el uno al otro a la vez; entradas hostiles al analizador de redes (cero bits, loopback, metadatos, IPv4 mapeada en IPv6, zonas, listas gigantes); topes y cuotas fuera de rango; y una passkey creada y usada por un autenticador virtual real de Chromium.

## Hallazgos

| # | Severidad | Hallazgo | Estado |
|---|---|---|---|
| 1 | Media | Carrera al retirar administradores: la comprobación "no es el último administrador" y el cambio eran dos pasos. Dos administradores que se deshabilitaban o degradaban entre sí a la vez podían dejar la instancia sin ninguno | Corregido: `changeAdmin` toma un bloqueo asesor de transacción y cuenta dentro de ella; regresión con 15 rondas concurrentes y pruebas de cada paso de la transacción |
| 2 | Media | El panel de administración permitía añadir `127.0.0.0/8` o `::1/128` a las redes permitidas: con eso cualquier miembro podía apuntar un sitio al propio servidor y a lo que escucha a su lado (por ejemplo la base de datos en el mismo contenedor) | Corregido: loopback entra en los rangos que el panel nunca abre; quien despliega puede seguir permitiéndolo a propósito con `TRANSFERLY_ALLOWED_NETWORKS`; regresión en `TestParseNetworks` |
| 3 | Baja | Un tope de ancho de banda de 1 B/s (o de unos pocos bytes) dejaría cada bloque de 32 KiB esperando horas y las peticiones colgadas | Corregido: `Validate` exige 0 o al menos 1 KiB/s; regresión en `TestService_Set` |
| 4 | Informativa | Faltaba una matriz que juntara el acceso entre equipos y la escalada de rol de la fase | Corregido: `TestPhaseThree_CrossWorkspaceAndEscalation`; sin fallos de fondo, solo ajustó los códigos esperados (`invalid_role`, `owner_bound`) |
| 5 | Baja | Quitar el segundo factor a una cuenta (por ella o por un administrador) no cierra las sesiones que ya estaban abiertas | Aceptado: las sesiones ya pasaron el segundo paso; cambiar la contraseña o deshabilitar la cuenta sí las cierra. La reposición del factor es una decisión de quien ya está dentro |
| 6 | Baja | `audit_events` no tiene retención: crece sin límite en instancias con mucha actividad de equipos | Aceptado: solo guarda cambios administrativos, no lecturas; la retención configurable y la purga se diseñan con la auditoría de acceso (D1) |
| 7 | Baja | Las ceremonias de passkey viven en memoria del proceso | Aceptado: es correcto con un solo nodo; con varios nodos (fase 5) deben ir a la base o a un almacén compartido, o usar sesiones pegajosas |
| 8 | Baja | Quien conoce la contraseña y tiene acceso al proceso puede leer la clave de instancia y derivar la clave que sella las semillas TOTP | Aceptado: mismo límite de confianza que la clave del vault de las sesiones abiertas (fases 1 y 2); la rotación de la clave de instancia queda para la fase 5 |
| 9 | Informativa | Una cuota es una lectura seguida de una escritura: dos peticiones simultáneas pueden pasarse por una unidad | Aceptado: las cuotas protegen contra abuso normal, no son un control de seguridad estricto; no dan acceso a nada |
| 10 | Informativa | Un miembro retirado conserva su propio historial de actividad (hallazgo 6 de la fase 2) | Cerrado: solo ve lo que hizo él mismo y la información de las transferencias es suya; sin cambios |

## Riesgos aceptados

Los hallazgos 5 a 10. Además: que un administrador de la instancia ve todas las cuentas y puede deshabilitarlas, cambiar roles y quitar segundos factores (todo queda auditado, y no puede tocar su propia cuenta ni al último administrador); que un miembro con la clave del workspace puede copiar sus secretos mientras es miembro (la rotación protege lo posterior a su salida, no lo que ya vio); y que el correo con el enlace de invitación lo envía a mano quien invita, por lo que el enlace debe compartirse por un canal de confianza.

## Conclusión

No quedan hallazgos altos ni críticos abiertos. La revisión encontró dos fallos medios reales, ambos corregidos con regresión (la carrera del último administrador y el loopback en las redes permitidas), y un endurecimiento menor (tope mínimo de ancho de banda). La matriz de acceso entre equipos y de escalada de rol queda como barrera automática. La fase 3 queda cerrada desde el punto de vista de seguridad; la fase 4 debe revisar XSS en vistas previas y editor, pérdida de datos en la sincronización, fuga de secretos al exportar y el abuso de proxies como pivote de red.
