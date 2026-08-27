// Package ortcore is the ONNX Runtime FFI boundary of goembed — the
// only package in this module with unsafe, uintptr, and generated
// offsets. Read ARQUITETURA_OFICIAL.md and CLAUDE.md in the repository
// root before changing anything here.
package ortcore

//go:generate go run ../internal/ortgen -out ortapi_gen.go
