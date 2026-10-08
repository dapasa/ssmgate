# Guion del vídeo (≈ 8–10 min)

Grabación de pantalla obligatoria; cámara opcional. Prepara antes:

- Terminal con letra grande y `ssmgate` compilado en el `PATH`.
- Las slides abiertas en modo presentación.
- El repo abierto en GitHub (README, pestaña Actions, Releases).
- Opcional: un config real en otra ruta, por si haces la demo contra AWS.

| Min. | Pantalla | Qué contar |
|---|---|---|
| 0:00 | Slide portada | Quién eres y qué es ssmgate en una frase. |
| 0:30 | Slide "El problema" | Bastion SSH: puerto 22, claves compartidas, poca trazabilidad. |
| 1:30 | Slide "La solución" + "Tres comandos" | SSM Session Manager y cómo ssmgate lo reduce a un comando. |
| 2:30 | Slide "Cómo funciona" | Recorrido de `ssmgate db`: SSO → rol → túnel → token IAM → psql. |
| 3:30 | **Terminal** | `ssmgate --demo envs`<br>`ssmgate --demo shell bastion --env dev` → `whoami`, `hostname`, `exit`<br>`ssmgate --demo db --env dev` → `select current_user;`, `\conninfo`, `\q`<br>`ssmgate --demo tunnel --env prod` → muestra la confirmación, escribe `prod` y luego Ctrl-C<br>`ssmgate db` sin `--env` → error explicativo |
| 5:30 | **Editor** | `ssmgate config init`, el YAML de ejemplo, un error a propósito (p. ej. `via: nada`) y `ssmgate config validate` mostrando todos los errores. |
| 6:30 | **Código** | `internal/connector/connector.go` (la interfaz), `internal/awsx/connector.go` frente a `internal/demo/demo.go`, y un test de `cmd/cmd_test.go`. |
| 7:30 | **GitHub** | Workflow de CI en verde (3 SO) y Releases con los 6 binarios. |
| 8:00 | Slide "Desarrollo con IA" | Cómo usaste Claude Code, cómo verificaste el código y un ejemplo concreto. |
| 9:00 | Slides "Retos" + "Gracias" | Retos, próximos pasos y enlaces. |

Consejo: ensaya la demo una vez. En modo demo hay pequeñas pausas
simuladas entre pasos, pensadas para que se pueda seguir en vídeo.
