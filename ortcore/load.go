package ortcore

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// pinnedORTVersion is the ONNX Runtime version this package's
// generated offsets and types (ortapi_gen.go) were produced against
// (ARQUITETURA_OFICIAL.md §6.7). Load fails closed — via checkVersion
// — if the loaded library reports a different version (§5.4): the
// stable-offset behavior observed between 1.24.3 and 1.28.0 (§6.7.1)
// is an artifact of the ABI being additive over that specific
// version range, not a guarantee the C API makes for all future
// versions.
const pinnedORTVersion = "1.28.0"

// Env is a loaded ONNX Runtime environment: an open library handle
// plus a created OrtEnv. It owns both and must be closed with Close
// when no longer needed. The zero Env is not usable — obtain one only
// via Load.
type Env struct {
	lib    uintptr
	apiPtr uintptr
	env    uintptr

	createSessionOptions  fnCreateSessionOptions
	createSession         fnCreateSession
	getErrorMessage       fnGetErrorMessage
	releaseStatus         fnReleaseStatus
	releaseEnv            fnReleaseEnv
	releaseSessionOptions fnReleaseSessionOptions
}

// LoadOption configures Load.
type LoadOption func(*loadOptions)

type loadOptions struct {
	libraryPath string
}

// WithLibraryPath sets an explicit path to libonnxruntime.so, taking
// priority over $ONNXRUNTIME_LIB_PATH and the standard search
// locations (ARQUITETURA_OFICIAL.md §3.1).
func WithLibraryPath(path string) LoadOption {
	return func(o *loadOptions) { o.libraryPath = path }
}

// Load resolves and safely opens the ONNX Runtime shared library
// (§3.1 discovery order, §3.3 safety checks), verifies it reports the
// version this package was built against (§5.4), and creates an
// OrtEnv. The returned *Env must be closed with Close when no longer
// needed.
func Load(opts ...LoadOption) (*Env, error) {
	var o loadOptions
	for _, opt := range opts {
		opt(&o)
	}

	path, err := resolveLibraryPath(o.libraryPath)
	if err != nil {
		return nil, err
	}

	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("ortcore: dlopen %s: %w", path, err)
	}

	var getApiBase func() uintptr
	purego.RegisterLibFunc(&getApiBase, lib, "OrtGetApiBase")
	basePtr := getApiBase()
	if basePtr == 0 {
		purego.Dlclose(lib)
		return nil, fmt.Errorf("ortcore: %s: OrtGetApiBase returned nil", path)
	}

	// §5.4: version checked BEFORE anything else, using offsets that
	// live in OrtApiBase (reachable without any offset generated
	// against OrtApi itself — §2.3).
	var getVersionString fnGetVersionString
	purego.RegisterFunc(&getVersionString, *(*uintptr)(unsafe.Pointer(basePtr + offBaseGetVersionString)))
	reported := goStringFromC(getVersionString())
	if err := checkVersion(reported, pinnedORTVersion); err != nil {
		purego.Dlclose(lib)
		return nil, fmt.Errorf("ortcore: %s: %w", path, err)
	}

	var getApi fnGetApi
	purego.RegisterFunc(&getApi, *(*uintptr)(unsafe.Pointer(basePtr + offBaseGetApi)))
	apiPtr := getApi(ortAPIVersion)
	if apiPtr == 0 {
		purego.Dlclose(lib)
		return nil, fmt.Errorf("ortcore: %s: reports version %s but GetApi(%d) returned nil", path, reported, ortAPIVersion)
	}

	e := &Env{lib: lib, apiPtr: apiPtr}
	fnAt := func(off uintptr) uintptr { return *(*uintptr)(unsafe.Pointer(apiPtr + off)) }

	var createEnv fnCreateEnv
	purego.RegisterFunc(&createEnv, fnAt(offCreateEnv))
	purego.RegisterFunc(&e.createSessionOptions, fnAt(offCreateSessionOptions))
	purego.RegisterFunc(&e.createSession, fnAt(offCreateSession))
	purego.RegisterFunc(&e.getErrorMessage, fnAt(offGetErrorMessage))
	purego.RegisterFunc(&e.releaseStatus, fnAt(offReleaseStatus))
	purego.RegisterFunc(&e.releaseEnv, fnAt(offReleaseEnv))
	purego.RegisterFunc(&e.releaseSessionOptions, fnAt(offReleaseSessionOptions))

	logID := cString("goembed")
	st := createEnv(2 /* ORT_LOGGING_LEVEL_WARNING */, logID, &e.env)
	if err := e.checkStatus(st); err != nil {
		purego.Dlclose(lib)
		return nil, fmt.Errorf("ortcore: CreateEnv: %w", err)
	}
	if e.env == 0 {
		purego.Dlclose(lib)
		return nil, fmt.Errorf("ortcore: CreateEnv returned a nil OrtEnv without an error status")
	}

	return e, nil
}

// checkVersion is the §5.4 invariant, extracted as a pure function so
// it is unit-testable without dlopen. reported is the raw string
// GetVersionString returns (e.g. "1.28.0"). The comparison is exact,
// not semver-flexible — matching the pin-a-single-version philosophy
// of §6.7.
func checkVersion(reported, pinned string) error {
	if reported != pinned {
		return fmt.Errorf("%w: loaded library reports version %q, this package was generated against %q",
			ErrVersionMismatch, reported, pinned)
	}
	return nil
}

// checkStatus copies an OrtStatus' error message and releases it. Per
// ARQUITETURA_OFICIAL.md §3.5/§5.2: GetErrorMessage and ReleaseStatus
// are called from HERE ONLY — no other function in this package calls
// either. Memory of ORT's making is copied and freed in the same
// place it was born; it never crosses this function's boundary.
func (e *Env) checkStatus(status uintptr) error {
	if status == 0 {
		return nil
	}
	msg := goStringFromC(e.getErrorMessage(status))
	e.releaseStatus(status)
	return fmt.Errorf("ort: %s", msg)
}

// Close releases the OrtEnv and closes the dlopen'd library handle.
// Close is safe to call more than once; subsequent calls are no-ops.
func (e *Env) Close() error {
	if e.env != 0 {
		e.releaseEnv(e.env)
		e.env = 0
	}
	if e.lib != 0 {
		if err := purego.Dlclose(e.lib); err != nil {
			return fmt.Errorf("ortcore: dlclose: %w", err)
		}
		e.lib = 0
	}
	return nil
}

// cString copies a Go string into a new null-terminated byte buffer
// and returns its address as a uintptr, for passing as a C `const
// char*` argument. The returned buffer is not retained by this
// package after the call it's used in returns; ONNX Runtime's
// CreateEnv does not document retaining its logid argument beyond the
// call (unlike, for example, output buffers it explicitly says must
// be freed by the caller), so no runtime.Pinner is needed here — §3.6
// governs buffers ORT retains across calls (tensor data, from J4 on),
// not this one.
func cString(s string) uintptr {
	b := append([]byte(s), 0)
	return uintptr(unsafe.Pointer(&b[0]))
}

// goStringFromC copies a null-terminated C string at the given
// address into a new Go string. Used for strings ORT hands back
// (GetVersionString, GetErrorMessage) — the copy happens before any
// Release* call frees the underlying memory (§3.5).
func goStringFromC(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Pointer(ptr + uintptr(n))) != 0 {
		n++
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n)
	out := make([]byte, n)
	copy(out, buf)
	return string(out)
}
