package ortcore

import "errors"

// Sentinel errors returned by Load and its helpers. Each is wrapped
// with additional context via fmt.Errorf's %w, so callers can still
// match with errors.Is(err, ortcore.ErrX).
var (
	// ErrLibraryNotFound means no candidate path (explicit,
	// $ONNXRUNTIME_LIB_PATH, or a standard location) resolved to an
	// existing, statable file.
	ErrLibraryNotFound = errors.New("ortcore: libonnxruntime not found")

	// ErrRelativePath means a candidate library path was not absolute.
	ErrRelativePath = errors.New("ortcore: library path must be absolute")

	// ErrPathTraversal means a candidate library path contained a
	// literal ".." path component.
	ErrPathTraversal = errors.New(`ortcore: library path must not contain ".." components`)

	// ErrWorldWritable means the resolved library file is writable by
	// users other than its owner. dlopen of such a file is treated as
	// a security risk (ARQUITETURA_OFICIAL.md §3.3) and refused.
	ErrWorldWritable = errors.New("ortcore: library file must not be writable by other users")

	// ErrVersionMismatch means the loaded ONNX Runtime reports a
	// version different from the one this package's generated offsets
	// (ortcore/ortapi_gen.go) were built against
	// (ARQUITETURA_OFICIAL.md §5.4, §6.7).
	ErrVersionMismatch = errors.New("ortcore: onnxruntime version does not match the version this package was built for")
)
