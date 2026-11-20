# tui-ssh

TUI multiplataforma en Go que convierte `~/.ssh/config` en un menú interactivo:
lista los hosts configurados, los filtra por etiquetas, abre sesiones SSH y levanta
port-forwarding en segundo plano mostrando cuáles están activos. Funciona con
teclado y ratón.

Estado actual: **v0.1.0**. La TUI lista hosts reales, resuelve su configuración
efectiva con `ssh -G`, abre sesiones interactivas y gestiona túneles locales,
remotos y dinámicos con re-adopción al reabrir.

## Features

- **Hosts:** lee `~/.ssh/config` y sus `Include` (globs, anidados, ciclos
  detectados), con filtro incremental por alias, etiqueta o `HostName` y filtro por
  etiqueta con `[`/`]`.
- **Etiquetas propias:** `tags.json` en XDG, editable desde la TUI con `T`. El
  archivo de ssh nunca se escribe.
- **Detalle honesto:** resuelve `hostname`, `user`, `port`, `identityfile` y
  `proxyjump` con `ssh -G -F`, y cae a una resolución marcada como *aproximada* si
  el binario no está.
- **Sesión SSH:** `enter` suspende la TUI y entrega el terminal al binario `ssh`,
  así que servidores con passphrase, MFA y ssh-agent funcionan sin reimplementar
  nada.
- **Túneles:** `-L`, `-R` y `-D` como masters de OpenSSH con control socket, estado
  `active`/`stopped` real (`ssh -O check`), logs, puerto efímero detectado y parada
  limpia (`ssh -O exit`).
- **Persistencia:** los túneles siguen vivos al cerrar la TUI y se re-adoptan al
  reabrir; al salir se pregunta si detenerlos.
- **Config a la vista:** pestaña con los archivos leídos, el recuento de hosts, los
  warnings del parser y el texto crudo numerado; `E` lo abre en `$EDITOR`.
- **Temas y atajos configurables:** cuatro temas y overrides de teclado mediante un
  `config.json` en XDG; `--list-themes` y `--list-keys` muestran los catálogos.
- **Multiplataforma y bajo consumo:** binarios estáticos (`CGO_ENABLED=0`) para
  linux/darwin/windows en amd64 y arm64.

## Stack

| Componente | Versión | Import |
| --- | --- | --- |
| Go | 1.27.4 | — |
| Bubble Tea | v2.1.2 | `charm.land/bubbletea/v2` |
| Lip Gloss | v2.1.1 | `charm.land/lipgloss/v2` |
| Bubbles | v2.2.1 | `charm.land/bubbles/v2` |
| GoReleaser | v2.20.2 | — |
| Air (hot reload) | v1.67.4 | — |
| Alpine (imagen de runtime) | 3.24.4 | — |

Las dependencias usan los paths canónicos v2 (`charm.land/...`), no los antiguos
`github.com/charmbracelet/...`. No hay ninguna librería de cliente SSH: la sesión y
los túneles delegados en el binario `ssh`.

## Estructura del proyecto

```
tui-ssh/
├── .github/workflows/     # ci.yml (gate) y release.yml (tags v*)
├── .agents/               # reglas por área
├── cmd/tui-ssh/           # composition root
├── internal/
│   ├── adapters/          # sshconfig, resolver, sshcmd, tags y tunnel
│   ├── config/            # flags y validación
│   ├── domain/            # tipos, límites y errores puros
│   ├── ports/             # interfaces de servicios
│   ├── testutil/fakes/    # dobles de test
│   └── tui/
│       ├── app/           # modelo raíz, pestañas, layout y ratón
│       ├── common/        # primitivas de celda y modales
│       ├── components/    # hostlist, hostdetail, tunnelpanel y configview
│       ├── keymap/        # registro de acciones, parser de teclas y overrides
│       └── styles/        # catálogo de temas y tokens visuales
├── scripts/               # install.sh (Linux/macOS) e install.ps1 (Windows)
├── .air.toml
├── .goreleaser.yaml
├── AGENTS.md
├── CHANGELOG.md
├── CONTRIBUTING.md
├── Dockerfile
├── LICENSE
├── Makefile
├── README.md
├── SECURITY.md
├── compose.yaml
├── config.example.json
├── go.mod
└── go.sum
```

`domain` y `ports` son puros; `adapters` y `tui` implementan la funcionalidad.
`main` es el único composition root.

## Requisitos

Para **usar** el binario solo necesitas Linux, macOS o Windows en amd64 o arm64, y
el binario `ssh` en el `PATH` (el de OpenSSH). El binario es estático
(`CGO_ENABLED=0`).

Para **desarrollar** el proyecto sí necesitas Docker y Docker Compose v2 o
superior: todo se compila y se prueba dentro de contenedores, sin Go en el host.

## Instalación

### Instalador (recomendado)

Linux y macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/MiguelFPP/tui-ssh/main/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/MiguelFPP/tui-ssh/main/scripts/install.ps1 | iex
```

El instalador detecta el sistema y la arquitectura, descarga el archivo de la
última release, **verifica el SHA-256** contra `checksums.txt` y deja el binario en:

| Sistema | Destino por defecto |
| --- | --- |
| Linux/macOS, como root | `/usr/local/bin` |
| Linux/macOS, usuario normal | `~/.local/bin` (avisa si falta en el `PATH`) |
| Windows | `%LOCALAPPDATA%\Programs\tui-ssh` (lo añade al `PATH` de usuario) |

Opciones de `install.sh`: `--version vX.Y.Z`, `--dir DIR` y `--no-verify`.
Opciones de `install.ps1`: `-Version`, `-Dir`, `-NoVerify` y `-NoPath`.

### Desde el código fuente

Si prefieres compilar tú mismo y ya tienes Go 1.27.4 o superior:

```bash
git clone https://github.com/MiguelFPP/tui-ssh.git
cd tui-ssh
go build -trimpath -o tui-ssh ./cmd/tui-ssh
```

Con `go build` sin ldflags el binario funciona pero `--version` informa `0.1.0`
sin commit. Para inyectar la versión igual que GoReleaser:

```bash
go build -trimpath \
  -ldflags "-s -w -X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD)" \
  -o tui-ssh ./cmd/tui-ssh
```

### Build local en contenedor

```bash
make build   # construye las imágenes Docker (dev y runtime)
make run     # ejecuta el TUI desde la imagen de producción (requiere TTY)
```

## Quickstart

```bash
tui-ssh                       # lee ~/.ssh/config
tui-ssh --ssh-config /ruta/config
tui-ssh --theme dracula
```

El TUI necesita un TTY real: usa `docker compose run` (o los targets `make dev`,
`make run`) y no `docker compose up -d`.

## Docker y `~/.ssh`

Los servicios `dev` y `app` **no** montan `~/.ssh`: leer las claves privadas del
usuario es un riesgo que solo debe asumirse a propósito. Para trabajar con hosts
reales están los perfiles opt-in:

- `make dev-ssh` arranca `dev-ssh`: monta `~/.ssh` **read-only**, expone
  `SSH_AUTH_SOCK` y usa `network_mode: host` para que los puertos se abran en el
  host y no dentro del contenedor.
- `make run-ssh` hace lo mismo con la imagen de producción.

Ver [`SECURITY.md`](SECURITY.md).

## Configuración

La configuración vive en un único archivo JSON y se resuelve en tres niveles —
flags, variables de entorno y archivo— con esta correspondencia:

| Dato | Flag | Env | Archivo | Default |
| :--- | :--- | :--- | :--- | :--- |
| Ruta del archivo | `--config FILE` | `TUI_SSH_CONFIG` | — | `$XDG_CONFIG_HOME/tui-ssh/config.json` |
| Config SSH | `--ssh-config FILE` | `TUI_SSH_SSHCONFIG` | — | `~/.ssh/config` |
| Binario ssh | `--ssh CMD` | `SSH` | — | `ssh` |
| Runtime de túneles | `--runtime-dir DIR` | `TUI_SSH_RUNTIME_DIR` | — | `$XDG_RUNTIME_DIR/tui-ssh` |
| Tema | `--theme NAME` | `TUI_SSH_THEME` | `theme` | `tokyonight-night` |
| Ancho de la lista | — | — | `list_width` | `0` (automático, 38 %) |
| Lista minimizada | — | — | `list_collapsed` | `false` |
| Layout del detalle | — | — | `detail_layout` | `split` |
| Atajos | — | — | `keys` | defaults del registro |

La precedencia es **flags > env > archivo > defaults**. Si el archivo por defecto
no existe, se ignora sin error; si `--config` apunta a uno inexistente, es un
error. El JSON es estricto: campos desconocidos, JSON inválido, acciones
desconocidas, teclas inválidas o conflictos en un mismo scope producen un error
accionable.

`tags.json` y `forwards.json` viven junto al config y se escriben de forma atómica
con permisos `0600`.

### Archivo base

```bash
tui-ssh --init-config          # escribe la plantilla
tui-ssh --init-config --force  # sobrescribe
tui-ssh --print-config         # imprime la configuración resuelta
```

### Temas

| Nombre | Descripción | Default |
| :--- | :--- | :---: |
| `tokyonight-night` | Tokyo Night | Sí |
| `catppuccin-mocha` | Catppuccin Mocha | — |
| `dracula` | Dracula | — |
| `gruvbox-dark` | Gruvbox Dark | — |

## Pestañas y atajos

Los atajos de las tablas son los **defaults**: se pueden sobrescribir en
`config.json` y `--list-keys` muestra el ID de cada acción. La ayuda `?` refleja
siempre las teclas efectivas.

| Atajo | Acción |
| --- | --- |
| `1` / `2` / `3` | Cambiar de pestaña (`Hosts`, `Túneles`, `Config`) |
| clic en la pestaña | Cambiar de pestaña |
| `tab` / `shift+tab` | Alternar entre lista y detalle |
| `F2` | Minimizar la lista o volver a mostrarla |
| `shift+left` / `shift+right` | Redimensionar la lista dos celdas |
| `L` | Alternar una o dos columnas del detalle |
| `?` | Mostrar u ocultar la ayuda |
| `,` | Ver la configuración (overlay read-only) |
| `esc` | Cerrar la ayuda o reenviar la tecla al panel |
| `q` / `ctrl+c` | Salir |

### Hosts

| Atajo | Acción |
| --- | --- |
| `↑`/`↓`, `j`/`k` | Mover el cursor por la lista |
| `pgup`/`pgdown`, `home`/`end` | Moverse por páginas o a los extremos |
| `/` | Filtrar por alias, etiqueta o `HostName` |
| `esc` | Limpiar el filtro |
| `[` / `]` | Ciclar el filtro por etiqueta |
| `enter` | Abrir la sesión SSH del host seleccionado |
| `t` | Levantar su primer forward declarado (o declarar uno) |
| `T` | Editar sus etiquetas |
| `n` | Declarar un forward |
| `y` | Avisar del comando `ssh` del host |
| `E` | Abrir el config en `$EDITOR` |
| `r` | Recargar hosts e invalidar la resolución |

### Túneles

| Atajo | Acción |
| --- | --- |
| `↑`/`↓`, `j`/`k` | Mover el cursor por la tabla |
| `s` / `x` / `R` | Levantar, detener (con confirmación) y reiniciar |
| `l` | Ver los logs del túnel |
| `a` | Alternar entre los túneles del host y todos |
| `r` | Refrescar el estado de los masters |
| `enter` | Abrir una sesión SSH al host del túnel |

### Config

| Atajo | Acción |
| --- | --- |
| `↑`/`↓`, `pgup`/`pgdown`, `home`/`end` | Desplazar el texto |
| `r` | Recargar el reporte |
| `E` | Abrir el config en `$EDITOR` |

## Túneles

Un túnel se declara con la forma compacta del prompt:

```
nombre=L bindPort:host:port        # local: -L
nombre=R bindPort:host:port        # remoto: -R
nombre=D port                      # dinámico: -D
local 0.0.0.0:8080:web:80          # dirección explícita
```

Se guardan en `forwards.json` y el primer forward se levanta al declararlo.

Cada túnel es un master de OpenSSH en segundo plano con su propio `ControlPath`.
El estado se consulta con `ssh -O check` y se apaga con `ssh -O exit`: nunca se
adivina un PID. Un puerto ocupado falla en el acto (`ExitOnForwardFailure=yes`) en
vez de quedar como túnel fantasma.

Los masters y su manifiesto viven en `$XDG_RUNTIME_DIR/tui-ssh`. Al cerrar la TUI
se pregunta si detenerlos; si no se detienen, la siguiente sesión los re-adopta
validándolos con `-O check`.

Un bind en `0` pide un puerto efímero: el master imprime `Allocated port N` y la
tabla muestra el puerto real.

## Arquitectura

Arquitectura **hexagonal (puertos y adaptadores)** para el I/O y **Elm
Architecture (MVU)** para la TUI; `main` es el único composition root.

- `internal/domain/` y `internal/ports/` son puros: no importan Bubble Tea, Lip
  Gloss ni `os/exec`.
- `internal/adapters/` implementa los puertos. `internal/tui/` compone los
  sub-modelos MVU y solo conoce `domain` y `ports`.
- `Update`/`View` no bloquean con I/O: toda operación externa va en un `tea.Cmd`
  con `context.Context` que devuelve un mensaje tipado.
- `ssh` se ejecuta con el binario nativo vía `os/exec`, con argumentos separados y
  sin shell. La resolución autoritativa es `ssh -G -F <config>`.

## Límites y métricas

- **Config:** acotado a 1 MiB, profundidad de `Include` 8 y líneas de 4 KiB.
- **Hosts:** límite de entradas y de opciones resueltas por host.
- **Túneles:** límite de túneles activos; logs acotados por bytes y líneas.
- **Polling:** el ticker del panel solo existe con la pestaña visible.
- **Objetivo de consumo:** CPU inactiva ≈ 0 % y RAM moderada; el tick de una
  pestaña oculta no reserva memoria (`BenchmarkInactiveTickNoPoll`).

## Calidad

| Comprobación | Resultado |
| :--- | :--- |
| `make test` (`-count=1`) | suite completa en verde |
| `make vet` | `go vet ./...` limpio |
| `make build` | imágenes dev y runtime |
| Smoke TUI | pseudo-TTY, salida limpia, exit 0 |

## Comandos

| Comando | Descripción |
| --- | --- |
| `make help` | Lista los targets disponibles (target por defecto) |
| `make dev` | Ejecuta el TUI con hot reload |
| `make dev-ssh` | Ejecuta el TUI con hot reload y `~/.ssh` read-only |
| `make run` | Ejecuta el TUI desde la imagen de producción |
| `make run-ssh` | Ejecuta el TUI de producción con `~/.ssh` y red del host |
| `make build` | Construye las imágenes Docker |
| `make fmt` | Ejecuta `go fmt ./...` |
| `make tidy` | Ejecuta `go mod tidy` |
| `make clean` | Borra los artefactos de Air |
| `make test` | Ejecuta `go test ./...`: solo CI y fase final |
| `make vet` | Ejecuta `go vet ./...`: solo CI y fase final |

### Targets por fase

| Comando | Descripción |
| --- | --- |
| `make test-pkg PKG='./internal/<pkg>/...'` | Tests de un paquete |
| `make test-run PKG='./internal/<pkg>/...' RUN='TestNombre'` | Tests que casan con `RUN` |
| `make vet-pkg PKG='./internal/<pkg>/...'` | `go vet` de un paquete |
| `make test-integration PKG='./internal/<pkg>/...' [RUN='...']` | Tests con build tag `integration` |
| `make bench-pkg PKG='./internal/<pkg>/...'` | Benchmarks de un paquete |
| `make test-race` | Suite completa con el detector de carreras: solo fase final |

## Flags

| Flag | Default | Descripción |
| --- | --- | --- |
| `--version` | — | Imprime `tui-ssh 0.1.0` y sale con código 0 |
| `--ssh-config FILE` | `~/.ssh/config` | Archivo de configuración de OpenSSH |
| `--ssh CMD` | `ssh` | Binario de ssh (o `$SSH`) |
| `--editor CMD` | `$EDITOR` o `vim` | Editor con el que se abre el config |
| `--runtime-dir DIR` | `$XDG_RUNTIME_DIR/tui-ssh` | Estado de los túneles |
| `--debug-log FILE` | vacío | Archivo de log de depuración |
| `--config FILE` | `$XDG_CONFIG_HOME/tui-ssh/config.json` | Archivo JSON de tema, layout y atajos |
| `--theme NAME` | `tokyonight-night` | Tema de color |
| `--list-themes` | — | Lista los temas disponibles y sale |
| `--list-keys` | — | Lista las acciones, su scope y sus teclas, y sale |
| `--init-config` | — | Escribe un archivo base y sale; no sobrescribe sin `--force` |
| `--force` | — | Permite sobrescribir con `--init-config` |
| `--print-config` | — | Imprime la configuración resuelta en JSON y sale |

## Alcance y limitaciones

- `~/.ssh/config` es solo lectura: los cambios se hacen en `$EDITOR`.
- El parser enumera bloques; no reimplementa la precedencia de OpenSSH. `Match`
  distinto de `all` se ignora con un aviso.
- Un túnel necesita clave sin passphrase o ssh-agent: `-f` no tiene TTY. Si el host
  pide credenciales, la UI lo explica y sugiere abrir una sesión primero.
- Los hosts con `ProxyJump` se conectan con normalidad porque la sesión delega en
  `ssh`; los túneles heredan esa cadena.

## Contribuir y seguridad

- Guía de contribución: [`CONTRIBUTING.md`](CONTRIBUTING.md).
- Política de seguridad y riesgo de montar `~/.ssh`: [`SECURITY.md`](SECURITY.md).
- Historial de cambios: [`CHANGELOG.md`](CHANGELOG.md).

## Licencia

MIT. Ver [`LICENSE`](LICENSE).
