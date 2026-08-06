# WI-03 — Protección HTTP/SSRF

Tracking: issue #6.

## DEFINE `/spec`

### Problema observable

Las tools web comparten un `http.Client` convencional. El cliente actual acepta cualquier destino cuyo string comience con `http`, delega resolución DNS y redirects al transporte estándar y no verifica la IP efectiva antes de conectar. Esto permite utilizar `web_fetch` o un backend configurable de búsqueda como proxy hacia loopback, redes privadas, link-local o endpoints de metadata. El límite actual de body tampoco informa truncamiento ni diferencia errores de policy, red y respuesta HTTP.

### Requisitos

- **SEC-NET-001:** únicamente se permiten URLs absolutas con scheme `http` o `https`, host válido y sin credenciales embebidas.
- **SEC-NET-002:** loopback IPv4/IPv6 debe rechazarse antes de emitir una conexión.
- **SEC-NET-003:** redes privadas, CGNAT, multicast, unspecified y rangos no enrutablemente públicos deben rechazarse.
- **SEC-NET-004:** link-local y direcciones conocidas de metadata deben rechazarse.
- **SEC-NET-005:** todos los resultados DNS deben validarse; el dial debe fijarse a una IP ya validada para evitar una segunda resolución insegura.
- **SEC-NET-006:** cada redirect debe revalidarse y la cadena debe tener un máximo estricto.
- **SEC-NET-007:** requests y bodies deben estar acotados por timeout y límite duro; contenido binario no permitido debe rechazarse.
- **SEC-NET-008:** errores de policy, red, cancelación, redirect, respuesta HTTP, body y content type deben ser distinguibles.

### Alcance

- introducir un `SafeHTTPClient` compartido por `web_fetch` y `web_search`;
- parsear y validar cada URL antes de construir o seguir una request;
- resolver todos los IPs del hostname y rechazar el destino si cualquiera es no público;
- conectar a una IP validada, preservando el hostname original para HTTP/TLS;
- revalidar redirects y eliminar headers sensibles al cambiar de host;
- definir timeout, máximo de redirects, máximo de body y content types permitidos;
- conservar `context.Context` hasta resolver, conectar, leer y cancelar;
- agregar errores tipados e identificables por código;
- mantener tests deterministas con resolver y dialer inyectables.

### No alcance

- crawling, robots.txt o rate limiting por dominio;
- autenticación genérica para endpoints arbitrarios;
- excepciones para servicios privados o SearXNG local;
- proxy corporativo configurable;
- integraciones internas confiables, que deberán usar adapters dedicados y policy explícita;
- hardening del servicio systemd, cubierto por WI-08.

### Invariantes

1. Ninguna tool web puede establecer una conexión a loopback, RFC1918, ULA, link-local, metadata, multicast, unspecified o CGNAT.
2. Todos los IPs devueltos por DNS deben ser públicos; una respuesta mixta se rechaza completa.
3. Un redirect público hacia un destino bloqueado se corta antes del segundo dial.
4. El transporte no vuelve a resolver el hostname después de validar DNS.
5. `Authorization`, cookies, proxy credentials y tokens de proveedores no cruzan a otro host mediante redirect.
6. El body nunca se consume sin límite.
7. La cancelación del contexto prevalece sobre retries o fallos de red.
8. No existe allowlist implícita para localhost o redes privadas.

### Criterios de aceptación

- loopback IPv4/IPv6, privadas, link-local y metadata son rechazadas sin invocar el transporte;
- un hostname con cualquier IP bloqueada es rechazado;
- redirect hacia privado se rechaza y no alcanza el endpoint final;
- schemes distintos de HTTP(S), userinfo y puertos inválidos se rechazan como policy;
- una respuesta pública textual puede recuperarse normalmente;
- un body mayor al límite se trunca de forma explícita para `web_fetch` y se rechaza para respuestas estructuradas de búsqueda;
- contenido binario se rechaza;
- timeout y cancelación producen errores identificables;
- la suite completa, repetición, race, vet y build quedan verdes.

## PLAN `/plan`

### Slice A — URL e IP policy

Tests RED:

- destinos loopback, privados, link-local y metadata;
- scheme no HTTP(S);
- credenciales embebidas;
- errores de policy antes de tocar el transporte.

GREEN mínimo:

- parser estricto de URL;
- clasificación centralizada de IPs;
- `HTTPError` con códigos estables.

### Slice B — DNS pinning y redirects

Tests RED:

- hostname que resuelve a privado;
- respuesta DNS mixta pública/privada;
- redirect público a privado;
- redirect loop;
- headers sensibles en cambio de host.

GREEN mínimo:

- resolver inyectable;
- validación de todos los resultados DNS;
- dial a IP validada;
- `CheckRedirect` acotado y revalidado.

### Slice C — Límites, contenido y tools

Tests RED:

- body mayor al límite;
- JSON truncado en search;
- content type binario;
- timeout y cancelación;
- respuesta HTTP >= 400.

GREEN mínimo:

- lectura `limit+1` con señal explícita;
- allowlist de media types textuales/estructurados;
- timeout global;
- integración del cliente en `web_fetch` y proveedores de `web_search`.

### Slice D — Regresión y evidencia

- suite de `internal/tools` repetida;
- race detector de `internal/tools`;
- suite completa, vet y builds;
- documentación de operación, riesgo residual y rollback;
- review del diff, threads y dependencias.

## Decisiones de seguridad

- Si DNS devuelve una mezcla de IP pública y bloqueada, el hostname completo se rechaza. No se intenta seleccionar solo la pública.
- Los endpoints SearXNG privados dejan de ser utilizables desde las tools web. Una integración privada futura requiere un adapter separado con configuración y policy propias.
- No se usa `http.ProxyFromEnvironment`, para impedir que un proxy implícito cambie el destino efectivo sin pasar por la policy.
- Los rangos reservados de documentación pueden usarse en tests con un dialer inyectado, pero producción exige IP global unicast no privada y fuera de rangos bloqueados explícitos.

## Riesgo residual

La policy valida DNS y fija el dial a la IP aprobada, reduciendo DNS rebinding. No sustituye aislamiento del proceso, egress firewall ni una allowlist empresarial. WI-08 debe complementar este control con usuario dedicado y restricciones del servicio.

## Rollback

Revertir el squash merge de WI-03. El rollback restaura el cliente HTTP convencional de WI-02; por seguridad, las tools web deberían deshabilitarse hasta reaplicar el fix.