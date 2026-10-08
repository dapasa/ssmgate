# Desarrollo asistido por IA

> **Borrador para revisar.** Completa y ajusta cada apartado con tu
> experiencia real (lo marcado con ✏️ lo tienes que escribir tú).

## Herramientas

- **Claude Code** (agente de programación de Anthropic) en terminal y en la
  app de escritorio.
- ✏️ _Otras herramientas de IA que hayas usado (Copilot, ChatGPT…)._

## Fases del proyecto y papel de la IA

### 1. Origen: de un script bash a una CLI en Go

✏️ _Explica el punto de partida: un script bash propio para conectarse por
SSM, sus limitaciones (dependía de la AWS CLI, no funcionaba igual en
Windows/macOS…) y cómo usaste la IA para portarlo a Go: diseño de los
subcomandos, el flujo OIDC device authorization con el SDK, la invocación del
`session-manager-plugin` y los problemas reales que surgieron (p. ej. un 403
por usar el mismo rol para todos los hosts, que llevó a definir un rol por
host)._

### 2. Generalización (sesión con Claude Code)

Objetivo: convertir una herramienta con valores fijos en un producto
genérico que pueda usar cualquier empresa.

Tareas delegadas al agente y revisadas por mí:

| Tarea | Resultado |
|---|---|
| Eliminar todos los datos de infraestructura del código | Nuevo paquete `config` con esquema YAML, valores por defecto y validación que informa de todos los errores a la vez |
| Desacoplar la CLI de AWS | Interfaz `connector.Connector` con dos implementaciones: `awsx` (real) y `demo` |
| Hacer el proyecto evaluable sin AWS | Modo `--demo` con túnel TCP real, shell y `psql` simulados |
| Soportar más casos de uso | Perfiles de AWS CLI además de SSO, `auth: password`, `ssl_mode` y `ca_bundle` configurables, entornos protegidos (`confirm`) |
| Nuevos comandos | `envs`, `config init/path/validate`, `doctor` |
| Tests | Tests unitarios y end-to-end del árbol de comandos |
| CI/CD | Lint + tests en 3 sistemas operativos + release con GoReleaser |
| Documentación | README completo para el TFM |

### 3. Cómo verifiqué lo que generaba la IA

- **Compilación, `go vet`, `golangci-lint` y tests** después de cada cambio.
  El agente los ejecutaba y corregía los fallos (por ejemplo, el linter
  señaló un `os.Exit` que se saltaba un `defer` y se reestructuró `main`).
- **Pruebas manuales del modo demo** de cada comando antes de dar el trabajo
  por bueno. Así salió un fallo real: la confirmación de `prod` leía la
  entrada con un buffer y se "comía" las líneas destinadas a la shell. Se
  cambió para leer byte a byte y se añadió un test.
- ✏️ _Prueba contra tu AWS real con tu propio config y anota el resultado._
- ✏️ _Revisión manual del diff: qué cambiaste o rechazaste de lo que propuso
  la IA y por qué._

## Lecciones aprendidas

✏️ _Ideas de partida, cámbialas por las tuyas:_

- Dar a la IA **contexto del dominio** (por qué `verify-ca` y no
  `verify-full`, por qué el usuario de BD es la parte local del email) evita
  que "arregle" cosas que funcionan a propósito.
- Pedir **tests y verificación ejecutable** en cada paso es lo que hace fiable
  el código generado.
- Diseñar una **interfaz pensada para tests** (el `Connector`) permitió que la
  IA escribiera tests end-to-end sin depender de AWS.
- La IA acelera mucho el trabajo repetitivo (CI, documentación, tests), pero
  las decisiones de diseño y de seguridad hay que revisarlas una a una.
