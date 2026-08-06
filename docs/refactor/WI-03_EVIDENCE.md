# WI-03 — Evidencia de protección HTTP/SSRF

Tracking: issue #6, PR #25.

## DEFINE

`WI-03_SPEC.md` define SEC-NET-001..008: URL policy, bloqueo de destinos no públicos, validación DNS completa, dial fijado a IP validada, redirects acotados, límites de respuesta y errores tipados.

## PLAN

El trabajo se ejecutó en cuatro slices:

1. URL/IP policy y clasificación de errores;
2. DNS pinning, redirects y protección de headers;
3. límites de body/content type, timeout y adopción por las tools;
4. repetición, race, suite completa, documentación y review.

## BUILD — RED

Head RED: `ace0df8356bd8d4e48d3a2c41d8919a93745d153`.

Formato y `go vet` pasaron. La suite falló porque `web_fetch` emitió requests y devolvió contenido exitoso para:

- `127.0.0.1`;
- `::1`;
- RFC1918;
- IPv6 ULA;
- `169.254.169.254`;
- `169.254.170.2`;
- `100.100.100.200`.

La falla demostró que el control anterior `strings.HasPrefix(url, "http")` no constituía una boundary SSRF.

## BUILD — GREEN

### URL e IP policy

- solo URLs absolutas `http`/`https`;
- host obligatorio, puertos válidos y credenciales embebidas rechazadas;
- localhost, loopback, private, ULA, link-local, multicast, unspecified, CGNAT y metadata bloqueados;
- IPv4-mapped IPv6 normalizado antes de clasificar.

### DNS y conexión

- resolver inyectable y testeable;
- todos los resultados DNS deben ser públicos;
- respuestas DNS mixtas se rechazan completas;
- el transporte conecta a la IP ya validada y no vuelve a resolver el hostname;
- proxy de ambiente deshabilitado.

### Redirects

- máximo de cinco redirects en producción;
- cada URL de redirect vuelve a pasar por URL policy;
- el destino DNS efectivo vuelve a validarse antes del dial;
- redirects hacia destinos privados se cortan antes de alcanzar el endpoint;
- `Authorization`, cookies, proxy credentials y tokens de providers se eliminan al cambiar autoridad.

### Límites y contenido

- timeout total de 15 segundos;
- body máximo duro de 512 KiB;
- `web_fetch.max_bytes` puede solicitar un límite menor y marca `[truncated]`;
- respuestas estructuradas truncadas de search se rechazan con `body_limit`;
- content types binarios se rechazan;
- cuerpos se leen mediante `LimitReader(limit+1)`.

### Errores

`HTTPError` diferencia:

- `policy_violation`;
- `network_failure`;
- `context_cancelled`;
- `timeout`;
- `redirect_limit`;
- `http_status`;
- `body_limit`;
- `content_type`.

## VERIFY

Head verificado: `0cc26be8ce72ea8f23b8dbfd6d479670f3f1d406`.

Workflow CI: run `31068612450`.

- baseline inventory: success;
- `gofmt -l .`: success;
- `go vet ./...`: success;
- `go test -count=1 -coverprofile=coverage.out ./...`: success;
- `go test -count=20 ./internal/tools`: success;
- `go test -race -count=1 ./internal/tools`: success;
- `go build ./cmd/assistant ./cmd/calibrate`: success;
- artefacto `coverage`: `sha256:32388cd8db9b1115348c27bb2728bb73e3e9add7c6d1f8240b4d5a7bf2e05e75`.

## Matriz de regresión

- loopback IPv4/IPv6;
- tres rangos RFC1918;
- IPv6 ULA;
- link-local y metadata;
- CGNAT, unspecified y multicast;
- scheme inválido, URL relativa, localhost, userinfo y puerto inválido;
- DNS privado y DNS mixto;
- dial fijado a IP validada;
- respuesta textual pública permitida;
- redirect público a privado;
- redirect loop;
- no filtración de headers sensibles entre hosts;
- body truncado y marcado;
- JSON estructurado truncado rechazado;
- content type binario rechazado;
- status HTTP tipado;
- cancelación y deadline;
- configuración inválida del cliente.

## REVIEW

- `web_fetch` y todos los providers de `web_search` usan el mismo cliente seguro;
- no hay fallback al `http.Client` anterior;
- el cliente opcional de test sigue tipado como `SafeHTTPClient`, por lo que el composition root no puede inyectar un cliente HTTP arbitrario;
- no se agregaron dependencias externas;
- no existe excepción para SearXNG privado;
- la policy no depende de strings de hostname después de resolver;
- la protección es concurrent-safe y superó race/repetición.

## Riesgo residual

El control reduce SSRF y DNS rebinding dentro del proceso, pero no reemplaza egress firewall, allowlists organizacionales ni aislamiento del servicio. WI-08 debe complementar el boundary mediante usuario dedicado y hardening de systemd.

## Rollback

Revertir el squash merge de PR #25. El rollback restaura el cliente web inseguro de WI-02; las tools web deben deshabilitarse si se ejecuta ese rollback.