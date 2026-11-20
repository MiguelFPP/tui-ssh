#!/bin/sh
# Installer for tui-ssh on Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/MiguelFPP/tui-ssh/main/scripts/install.sh | sh
#
#   sh install.sh --version v0.3.0 --dir "$HOME/bin"
#
# Options:
#   -v, --version <vX.Y.Z>  Version to install (default: latest release).
#   -d, --dir <path>        Install directory (default: /usr/local/bin as root,
#                           ~/.local/bin otherwise).
#       --no-verify         Skip the SHA-256 checksum verification.
#   -h, --help              Show this help.
#
# The script is POSIX sh and needs curl or wget plus tar.

set -eu

REPO="MiguelFPP/tui-ssh"
RELEASES="https://github.com/${REPO}/releases"
BIN="tui-ssh"

version=""
install_dir=""
verify=1

say() { printf '%s\n' "$*"; }
warn() { printf '%s\n' "tui-ssh: $*" >&2; }
die() {
	warn "$*"
	exit 1
}

usage() {
	cat <<'EOF'
Instala tui-ssh en Linux y macOS.

Uso:
  install.sh [opciones]

Opciones:
  -v, --version <vX.Y.Z>  Versión a instalar (por defecto, la última release).
  -d, --dir <ruta>        Directorio de instalación (por defecto /usr/local/bin
                          como root, ~/.local/bin en caso contrario).
      --no-verify         Omite la verificación del checksum SHA-256.
  -h, --help              Muestra esta ayuda.

Ejemplos:
  curl -fsSL https://raw.githubusercontent.com/MiguelFPP/tui-ssh/main/scripts/install.sh | sh
  sh install.sh --version v0.3.0 --dir "$HOME/bin"
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	-v | --version)
		[ $# -ge 2 ] || die "$1 requiere un valor"
		version="$2"
		shift 2
		;;
	-d | --dir)
		[ $# -ge 2 ] || die "$1 requiere un valor"
		install_dir="$2"
		shift 2
		;;
	--no-verify)
		verify=0
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		die "opción desconocida: $1 (prueba --help)"
		;;
	esac
done

tmpdir=$(mktemp -d) || die "no se pudo crear un directorio temporal"
trap 'rm -rf "$tmpdir"' EXIT INT TERM HUP

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2" 2>/dev/null
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1" 2>/dev/null
	else
		die "se requiere curl o wget"
	fi
}

detect_target() {
	os=$(uname -s)
	case "$os" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	MINGW* | MSYS* | CYGWIN*)
		die "Windows no está soportado por este script; usa scripts/install.ps1"
		;;
	*) die "sistema no soportado: $os" ;;
	esac

	arch=$(uname -m)
	case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) die "arquitectura no soportada: $arch" ;;
	esac
}

latest_version() {
	if command -v curl >/dev/null 2>&1; then
		url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "${RELEASES}/latest" 2>/dev/null || true)
		case "$url" in
		*/tag/v*) printf '%s' "${url##*/tag/v}" && return 0 ;;
		esac
	fi

	json="$tmpdir/latest.json"
	fetch "https://api.github.com/repos/${REPO}/releases/latest" "$json" 2>/dev/null || return 1
	tag=$(sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p' "$json" | head -n 1)
	[ -n "$tag" ] || return 1
	printf '%s' "$tag"
}

resolve_version() {
	[ -n "$version" ] || version=$(latest_version) ||
		die "no se pudo determinar la última versión; usa --version vX.Y.Z"

	version=${version#v}
	case "$version" in
	'' | *[!0-9.]*) die "versión inválida: $version" ;;
	esac
}

resolve_install_dir() {
	if [ -n "$install_dir" ]; then
		return
	fi

	if [ "$(id -u)" -eq 0 ]; then
		install_dir=/usr/local/bin
	else
		install_dir="$HOME/.local/bin"
	fi
}

verify_checksum() {
	archive=$1
	expected=$2

	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "$archive" | cut -d' ' -f1)
	elif command -v shasum >/dev/null 2>&1; then
		actual=$(shasum -a 256 "$archive" | cut -d' ' -f1)
	elif command -v openssl >/dev/null 2>&1; then
		actual=$(openssl dgst -sha256 "$archive" | sed 's/.*= *//')
	else
		warn "no se encontró ninguna herramienta sha256; se omite la verificación"
		return 0
	fi

	[ "$actual" = "$expected" ] ||
		die "el checksum no coincide para $archive (esperado $expected, obtenido $actual)"
}

say "→ detectando plataforma"
detect_target
resolve_version
resolve_install_dir

name="${BIN}_${version}_${os}_${arch}"
url="${RELEASES}/download/v${version}/${name}.tar.gz"

say "  versión:    ${version}"
say "  plataforma: ${os}/${arch}"
say "  destino:    ${install_dir}"

say "→ descargando ${name}.tar.gz"
fetch "$url" "$tmpdir/$name.tar.gz" ||
	die "falló la descarga: $url (¿está publicada v${version}? ver ${RELEASES})"

if [ "$verify" -eq 1 ]; then
	if fetch "${RELEASES}/download/v${version}/checksums.txt" "$tmpdir/checksums.txt" 2>/dev/null; then
		expected=$(awk -v f="${name}.tar.gz" '$2 == f { print $1; exit }' "$tmpdir/checksums.txt")
		if [ -n "$expected" ]; then
			say "→ verificando checksum"
			verify_checksum "$tmpdir/$name.tar.gz" "$expected"
		else
			warn "${name}.tar.gz no aparece en checksums.txt; se omite la verificación"
		fi
	else
		warn "checksums.txt no está disponible; se omite la verificación"
	fi
fi

say "→ extrayendo"
tar -xzf "$tmpdir/$name.tar.gz" -C "$tmpdir" "$BIN" ||
	die "no se pudo extraer $BIN del archivo"

mkdir -p "$install_dir" || die "no se pudo crear ${install_dir}"

# Write next to the target and rename: the rename replaces the inode, so an
# update also works while the current binary is running, and an interrupted
# install never leaves a half-written executable behind.
staged="$install_dir/.${BIN}.new.$$"
cp "$tmpdir/$BIN" "$staged" || {
	rm -f "$staged"
	die "no se pudo escribir en ${install_dir}"
}
chmod 0755 "$staged"
mv -f "$staged" "$install_dir/$BIN" || {
	rm -f "$staged"
	die "no se pudo reemplazar ${install_dir}/${BIN}"
}

say "✓ instalado ${BIN} ${version} en ${install_dir}/${BIN}"

case ":$PATH:" in
*":$install_dir:"*) ;;
*)
	warn "añade ${install_dir} al PATH para ejecutar ${BIN}:"
	warn "  export PATH=\"${install_dir}:\$PATH\""
	;;
esac

"$install_dir/$BIN" --version 2>/dev/null || true
