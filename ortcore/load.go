package ortcore

import (
	"fmt"
	"runtime"
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

	// logIDPin pins the heap-allocated C string passed to CreateEnv's
	// logid parameter (§3.6). It is unpinned only in Close, never by a
	// local defer in Load — whether ONNX Runtime retains this pointer
	// beyond the CreateEnv call itself is undocumented and unverified,
	// so the buffer is kept valid and address-stable for the Env's
	// entire lifetime, not just for the duration of one call.
	logIDPin runtime.Pinner
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
func Load(opts ...LoadOption) (env *Env, err error) {
	var o loadOptions
	for _, opt := range opts {
		opt(&o)
	}

	path, resolveErr := resolveLibraryPath(o.libraryPath)
	if resolveErr != nil {
		return nil, resolveErr
	}

	lib, dlopenErr := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if dlopenErr != nil {
		return nil, fmt.Errorf("ortcore: dlopen %s: %w", path, dlopenErr)
	}

	// purego.RegisterLibFunc/RegisterFunc panic — they don't return an
	// error — if a symbol is missing or a function pointer is invalid.
	// That happens for real if path is a valid, dlopen-able shared
	// library that just isn't ONNX Runtime (§3.3's checks validate the
	// file, not its contents). Recover here so that case reports a
	// normal error instead of crashing the caller's process, and so
	// lib is never leaked on this path either.
	defer func() {
		if r := recover(); r != nil {
			purego.Dlclose(lib)
			env = nil
			err = fmt.Errorf("ortcore: %s: does not look like a compatible ONNX Runtime library: %v", path, r)
		}
	}()

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

	logID := e.cStringPinned("goembed")
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
	e.logIDPin.Unpin() // safe to call even if never pinned or already unpinned
	if e.lib != 0 {
		lib := e.lib
		e.lib = 0
		if err := purego.Dlclose(lib); err != nil {
			return fmt.Errorf("ortcore: dlclose: %w", err)
		}
	}
	return nil
}

// cStringPinned copies s into a new null-terminated byte buffer,
// pins it via e.logIDPin so it survives both stack movement and (per
// §3.6) a hypothetical future moving garbage collector, and returns
// its address as a uintptr for passing as a C `const char*` argument.
//
// Pinning matters even for the duration of a single call: the plain
// append+unsafe.Pointer form previously used here put the buffer on
// the stack, and Go's stack copier does not adjust a uintptr — unlike
// a real pointer — when a goroutine's stack moves during a call
// (confirmed empirically while fixing this: forcing stack growth
// between allocating such a buffer and reading through its raw
// address reproducibly corrupted the read). Pinning forces the buffer
// onto the heap and keeps its address stable regardless of what the
// stack does.
//
// The pin is released only in Close, never by a local defer in the
// caller — whether ONNX Runtime retains this specific pointer beyond
// the call that receives it is undocumented, so the buffer is kept
// pinned for the Env's entire lifetime rather than assumed safe to
// release early.
func (e *Env) cStringPinned(s string) uintptr {
	b := append([]byte(s), 0)
	e.logIDPin.Pin(&b[0])
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
