# Defectos conocidos del baseline

Esta lista evita que el PR de importación sea interpretado como una aprobación operativa del código.

| Área | Defecto congelado | Work item propietario |
|---|---|---|
| Configuración | YAML permisivo, validación insuficiente y ceros ambiguos | WI-01 |
| Tool policy | Escritura y borrado disponibles sin policy central | WI-01 |
| Filesystem | Escape mediante symlinks y borrado de raíz | WI-02 |
| Red | `web_fetch` permite SSRF hacia destinos privados | WI-03 |
| Orquestación | Runtime real duplicado y limitado a una ronda adicional | WI-04 |
| LLM | Lifecycle, errores de request y cleanup incompletos | WI-05 |
| Memoria | Resumen por concatenación y crecimiento no acotado | WI-06 |
| Audio/App | Pipeline principal difícil de probar y captura multicanal inconsistente | WI-07 |
| Operación | `compose.yml` ausente, build/install inconsistentes y systemd sin usuario dedicado | WI-08 |

No utilizar `sudo make install` ni habilitar herramientas mutables con `fs_root` vacío hasta cerrar los work items de seguridad y operación correspondientes.
