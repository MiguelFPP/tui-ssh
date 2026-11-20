package domain

import "errors"

// Sentinels wrapped by adapters with %w; cancellation uses context errors instead.
var (
	ErrInvalidPath       = errors.New("ruta inválida")
	ErrInvalidArgument   = errors.New("argumento inválido")
	ErrNotFound          = errors.New("no encontrado")
	ErrConflict          = errors.New("conflicto")
	ErrConfigMissing     = errors.New("no hay archivo de configuración de ssh")
	ErrConfigInvalid     = errors.New("el archivo de configuración no es válido")
	ErrIncludeCycle      = errors.New("include circular")
	ErrTooLarge          = errors.New("contenido demasiado grande")
	ErrPermissionDenied  = errors.New("permiso denegado")
	ErrSSHUnavailable    = errors.New("ssh no está disponible")
	ErrTunnelExists      = errors.New("el túnel ya está activo")
	ErrTunnelFailed      = errors.New("el túnel no pudo arrancar")
	ErrTunnelNotFound    = errors.New("túnel no encontrado")
	ErrAuthRequired      = errors.New("el host pide autenticación interactiva")
	ErrEditorUnavailable = errors.New("el editor externo no está disponible")
)
