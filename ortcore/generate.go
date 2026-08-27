// Package ortcore is the ONNX Runtime FFI boundary of goembed — the
// only package in this module with unsafe, uintptr, and generated
// offsets. Read ARQUITETURA_OFICIAL.md and CLAUDE.md in the repository
// root before changing anything here.
package ortcore

// ORT_HEADER_DIR must point at the directory containing the pinned
// onnxruntime_c_api.h (ARQUITETURA_OFICIAL.md §6.7.1) before running
// `go generate` — internal/ortgen has no default search path.
//
//go:generate go run ../internal/ortgen -out ortapi_gen.go
