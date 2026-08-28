# J2 — ortcore: Load, checkStatus, CreateEnv Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first behavior of `ortcore`: `Load()` safely resolves
and opens `libonnxruntime.so`, verifies its reported version against the
pinned one, creates an `OrtEnv`, and exposes `Close()` — with every
`purego.RegisterFunc` binding built from the exact types J1 generated.

**Architecture:** Two new files with one responsibility each. `resolve.go`
is pure `path/filepath`/`os` logic — no `purego`, no dlopen — implementing
the §3.1 discovery order and the §3.3 safety checks, fully unit-testable
against `t.TempDir()` fixtures. `load.go` consumes a validated path,
`purego.Dlopen`s it, walks `OrtApiBase` → `OrtApi` exactly as the J0/J1
spikes proved works, and binds the handful of `fnX` types J1 generated
(`fnCreateEnv`, `fnCreateSessionOptions`, `fnCreateSession`,
`fnGetErrorMessage`, `fnReleaseStatus`, `fnReleaseEnv`,
`fnReleaseSessionOptions`) via `purego.RegisterFunc` — never
`purego.SyscallN` (§5.1/§6.1). `checkStatus` (§5.2) and `Close` are
methods on the returned `*Env`.

**Tech Stack:** Go 1.27.0, stdlib (`path/filepath`, `os`, `errors`,
`unsafe`), `github.com/ebitengine/purego@v0.10.2` (new dependency — not
yet in `go.mod`).

**Spec:** `ARQUITETURA_OFICIAL.md` (repository root) — §3.1 (discovery
order), §3.3 (path safety, security-critical), §3.5/§5.2 (memory
ownership invariant, `checkStatus`), §5.4 (version-check invariant),
§2.3 (`GetVersionString` lives in `OrtApiBase`, reachable without
offsets), §6.7/§6.7.1 (ORT 1.28.0 pinned), §6.8 item 3 (the J1 arity
guard checks arity only, not types — relevant here because `load.go` is
the first code that actually *calls* the generated `fnX` types via
`RegisterFunc`; a type-level mistake here is a **runtime** bug, not a
build-time one — see Task 2's manual verification step). Also read
`CLAUDE.md` before touching anything in `ortcore/`.

**Pre-plan verification (already done, informing every task below):**
before writing this plan, a throwaway spike (not committed) proved, on
this machine, against the real `libonnxruntime.so.1.28.0`:
- `purego.RegisterFunc` — not `SyscallN` — works correctly end-to-end
  with the exact `fnCreateEnv`/`fnCreateSessionOptions`/`fnCreateSession`/
  `fnGetErrorMessage`/`fnReleaseStatus`/`fnReleaseEnv`/
  `fnReleaseSessionOptions` types copied verbatim from
  `ortcore/ortapi_gen.go`: `CreateEnv` succeeds, the `CreateSession`
  error path round-trips through a `checkStatus`-shaped function
  correctly, and `Release*` calls don't crash.
- `purego.Dlclose(handle)` exists (cross-platform, unlike the
  no-portable-Dlclose assumption this plan almost shipped with) and
  closes the handle cleanly after `Release*`.
- `filepath.EvalSymlinks` on a nonexistent path returns a `*fs.PathError`
  detectable via `os.IsNotExist`; on a path containing a literal `..`
  segment it silently resolves it away (so rejecting `..` must happen on
  the **raw** input string, before resolution — a canonicalized path can
  never usefully be checked for `..`).
- `os.Stat().Mode().Perm()&0o002` correctly detects "other-writable" —
  but only when the file's mode was set via `os.Chmod` (or created by a
  process with an open umask); `os.WriteFile(path, data, 0o666)` is
  masked by the process umask and will **not** produce a world-writable
  file on a typical system. Test fixtures must use `os.Chmod` explicitly
  to force the condition, not rely on the `WriteFile` mode argument.
- `filepath.Join` calls `filepath.Clean` internally and silently
  collapses `..` segments — `filepath.Join(dir, "..", x, y)` never
  produces a path containing a literal `..`. A test asserting the
  `ErrPathTraversal` rejection must build the fixture path with plain
  string concatenation (`dir + "/../" + ...`), never `filepath.Join` —
  confirmed the hard way: an early draft of Task 1's
  `TestValidateLibraryPath_RejectsDotDot` used `filepath.Join` and
  silently tested nothing (it passed a already-cleaned path, so the
  function under test never saw a `..` at all — the test passed for
  the wrong reason before this was caught and fixed).
- **All of Task 1's code and every test in this plan were actually
  compiled and run** (not just written) in an isolated scratch copy of
  this repository, against the real cached ONNX Runtime 1.28.0
  library, before this plan was considered final — including the
  `filepath.Join` bug above, and the finding immediately below. This
  plan is not a description of code that should work; it is a
  transcription of code that was proven to work.
- **`go vet` (without flags) reports 5 false-positive "possible misuse
  of unsafe.Pointer" findings once `load.go` exists** — on
  `apiPtr+off`/`basePtr+off` dereferences. This is `go vet`'s
  `unsafeptr` analyzer, which exists to catch a `uintptr`-typed word
  holding a pointer value that becomes invisible to Go's GC/stack
  copying. That hazard cannot occur here: `apiPtr` and `basePtr` are
  addresses of ONNX Runtime's C-allocated memory, entirely outside the
  Go heap — the GC never manages or moves it. Confirmed by inspecting
  `purego`'s own source: `go vet ./...` on the `purego` module itself
  is clean, because it never dereferences a raw C address as
  `unsafe.Pointer` — it calls addresses via architecture-specific
  assembly instead (`syscall.Set(cfn, ...)`, taking `cfn` as a plain
  `uintptr`). `ortcore` genuinely needs to *read* memory at computed
  offsets (the `OrtApi` function-pointer table), not just call it, so
  that escape isn't available. `go vet -unsafeptr=false` resolves this
  cleanly for the whole module with no other change — this is now the
  project's canonical vet invocation, documented in `CLAUDE.md` §"Zona
  de alto escrutínio" item 6 so it is never mistaken for a real defect
  and "fixed" by disabling something load-bearing instead.

## Global Constraints

- Module: `github.com/pablobelmiro/goembed`. Go `1.27.0`, dependencies
  always the latest stable tag — `purego@v0.10.2` is current stable
  (§3.4a; the `main` branch is explicitly not used — §2.6.4).
- `CGO_ENABLED=0 go build ./... && go vet -unsafeptr=false ./... && go
  test ./...` must pass for the whole module at the end of every task
  (from Task 2 onward — see that task's "Pre-plan verification" note:
  plain `go vet` flags 5 false positives once `load.go` exists, because
  `apiPtr`/`basePtr` are addresses of ORT's C memory, not Go-managed
  memory, and `go vet`'s `unsafeptr` analyzer cannot tell the
  difference. `purego` itself avoids ever tripping this by calling
  raw addresses via assembly instead of dereferencing them as
  `unsafe.Pointer` — `ortcore` genuinely needs to *read* memory at
  computed offsets, not just call it, so that escape isn't available
  here. Confirmed empirically before this plan was written: `go vet
  ./...` on `purego`'s own source is clean; the flag is scoped to this
  module's real, new need starting at Task 2, not a blanket suppression
  carried over from anywhere else. Documented in `CLAUDE.md` §"Zona de
  alto escrutínio" item 6, so this isn't rediscovered from scratch
  later.).
- No test in this plan touches the network. Tests needing the real
  `.so` read `~/.cache/goembed/onnxruntime/1.28.0/` (same pattern as
  J1's `cachedHeaderDir` helper — skip with a clear message if absent,
  never fetch).
- `uintptr`/`unsafe.Pointer` never cross out of `ortcore` (§3.5) — this
  plan's public API (`Env`, `Load`, `LoadOption`, `Close`) exposes none.
- `GetErrorMessage` and `ReleaseStatus` are called **only** from
  `checkStatus` — no other call site, anywhere in the package (§3.5,
  §5.2). This is grep-verifiable and Task 2's self-review checks it.
- Every `purego` binding uses `RegisterFunc` with a named `fnX` type
  from `ortapi_gen.go` — never `SyscallN`, never an inline anonymous
  func type (§5.1/§6.1).
- Checksum verification of the loaded library (§3.3, listed as
  "opcionalmente") is **out of scope for this plan** — the spec marks
  it optional, and pinning a platform-specific hash now would add
  complexity disproportionate to what J2 needs. Left as a future
  `ARQUITETURA_OFICIAL.md` item if ever revisited, not silently forgotten.

---

## File Structure

```
ortcore/
├── errors.go          # new — sentinel errors shared by resolve.go and load.go (Task 1)
├── resolve.go          # new — §3.1 discovery order + §3.3 safety checks, no purego (Task 1)
├── resolve_test.go     # new — pure filesystem tests, t.TempDir() fixtures only (Task 1)
├── load.go              # new — Env, Load, checkStatus, Close; the only purego call sites (Task 2)
├── load_test.go         # new — Load against the real cached .so + checkVersion unit tests (Task 2)
├── generate.go          # existing (J1) — untouched
├── ortapi_gen.go         # existing (J1) — untouched, only consumed
└── ortapi_gen_test.go    # existing (J1) — untouched
```

`resolve.go` has zero dependency on `purego` or on any ORT type — it's
pure `path/filepath`/`os` logic, which is exactly why it's tested and
reviewed as its own task before anything touches the FFI boundary.

---

### Task 1: Path resolution and safety (`resolve.go`)

**Files:**
- Create: `ortcore/errors.go`
- Create: `ortcore/resolve.go`
- Test: `ortcore/resolve_test.go`

**Interfaces:**
- Consumes: nothing beyond stdlib.
- Produces: `resolveLibraryPath(explicit string) (string, error)` — the
  function Task 2's `Load()` calls to get a validated, canonical,
  symlink-resolved absolute path before ever calling `purego.Dlopen`.
  Also produces the five sentinel errors (`ErrLibraryNotFound`,
  `ErrRelativePath`, `ErrPathTraversal`, `ErrWorldWritable`,
  `ErrVersionMismatch` — the last one unused until Task 2, declared here
  since all sentinels live in one file) that Task 2 wraps and returns.

- [ ] **Step 1: Write `ortcore/errors.go`**

```go
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
```

- [ ] **Step 2: Write `ortcore/resolve.go`**

```go
package ortcore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// libraryPathEnvVar is the environment variable Load checks when no
// explicit path is given (ARQUITETURA_OFICIAL.md §3.1).
const libraryPathEnvVar = "ONNXRUNTIME_LIB_PATH"

// standardLibraryPaths are the conventional locations to probe on
// linux/amd64 — the only platform this package supports (§3.4) — when
// neither an explicit path nor $ONNXRUNTIME_LIB_PATH is set.
var standardLibraryPaths = []string{
	"/usr/lib/libonnxruntime.so",
	"/usr/lib/x86_64-linux-gnu/libonnxruntime.so",
	"/usr/local/lib/libonnxruntime.so",
}

// resolveLibraryPath implements the §3.1 discovery order and the §3.3
// safety checks. explicit is the caller-supplied path (empty if none).
//
// An explicit path or $ONNXRUNTIME_LIB_PATH that fails validation
// returns that candidate's specific error immediately — it does NOT
// fall through to a weaker source. A wrong explicit path is a
// configuration mistake that should surface loudly, not be silently
// papered over by guessing elsewhere. Only when neither is set does
// resolveLibraryPath try the standard locations, and only fails (with
// an aggregated, actionable error) if every one of them also fails.
func resolveLibraryPath(explicit string) (string, error) {
	return resolveLibraryPathWithCandidates(explicit, os.Getenv(libraryPathEnvVar), standardLibraryPaths)
}

// resolveLibraryPathWithCandidates is resolveLibraryPath with its
// inputs as parameters instead of reading the environment and the
// package-level default list — this is what tests call, so they can
// exercise the discovery order and the "all standard locations fail"
// path without touching the real environment or filesystem layout.
func resolveLibraryPathWithCandidates(explicit, envPath string, standardPaths []string) (string, error) {
	if explicit != "" {
		return validateLibraryPath(explicit)
	}
	if envPath != "" {
		return validateLibraryPath(envPath)
	}

	var errs []error
	for _, candidate := range standardPaths {
		path, err := validateLibraryPath(candidate)
		if err == nil {
			return path, nil
		}
		errs = append(errs, err)
	}
	return "", fmt.Errorf("%w: pass ortcore.WithLibraryPath(path), set $%s, or install libonnxruntime.so at one of %v (%w)",
		ErrLibraryNotFound, libraryPathEnvVar, standardPaths, errors.Join(errs...))
}

// validateLibraryPath applies the §3.3 checks to a single candidate
// path and returns its canonical, symlink-resolved absolute form if it
// passes all of them. Checks run in this order:
//  1. must be absolute (rejects a relative path outright — never
//     resolved relative to anything)
//  2. must not contain a literal ".." path component, checked on the
//     RAW string — filepath.EvalSymlinks silently resolves ".." away,
//     so checking after resolution would never catch anything
//  3. must resolve via filepath.EvalSymlinks to an existing file
//  4. the resolved file must not be writable by other users
func validateLibraryPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %q", ErrRelativePath, path)
	}
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		if seg == ".." {
			return "", fmt.Errorf("%w: %q", ErrPathTraversal, path)
		}
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrLibraryNotFound, path, err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrLibraryNotFound, resolved, err)
	}
	if info.Mode().Perm()&0o002 != 0 {
		return "", fmt.Errorf("%w: %q (mode %v)", ErrWorldWritable, resolved, info.Mode())
	}

	return resolved, nil
}
```

- [ ] **Step 3: Write the failing tests in `ortcore/resolve_test.go`**

```go
package ortcore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("fake .so"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	// os.Chmod, not the WriteFile mode argument — WriteFile's mode is
	// masked by the process umask, so it cannot reliably produce a
	// world-writable file (confirmed empirically before writing this
	// plan; see the plan's "Pre-plan verification" section).
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func TestValidateLibraryPath_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "libonnxruntime.so")
	writeFile(t, path, 0o644)

	got, err := validateLibraryPath(path)
	if err != nil {
		t.Fatalf("validateLibraryPath: %v", err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestValidateLibraryPath_ResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.so")
	writeFile(t, real, 0o644)
	link := filepath.Join(dir, "link.so")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := validateLibraryPath(link)
	if err != nil {
		t.Fatalf("validateLibraryPath: %v", err)
	}
	if got != real {
		t.Errorf("got %q, want the symlink's target %q", got, real)
	}
}

func TestValidateLibraryPath_RejectsRelative(t *testing.T) {
	_, err := validateLibraryPath("relative/libonnxruntime.so")
	if !errors.Is(err, ErrRelativePath) {
		t.Errorf("got %v, want ErrRelativePath", err)
	}
}

func TestValidateLibraryPath_RejectsDotDot(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.so")
	writeFile(t, real, 0o644)

	// Constructs a path that resolves to a real, valid file — proving
	// the ".." rejection is a policy check on the raw string, not a
	// side effect of the file not existing. filepath.Join is NOT used
	// here: it calls filepath.Clean internally and would silently
	// collapse ".." away before validateLibraryPath ever saw it —
	// confirmed empirically while writing this plan. Plain string
	// concatenation is required to preserve the literal ".." segment.
	withDotDot := dir + "/../" + filepath.Base(dir) + "/real.so"

	_, err := validateLibraryPath(withDotDot)
	if !errors.Is(err, ErrPathTraversal) {
		t.Errorf("got %v, want ErrPathTraversal", err)
	}
}

func TestValidateLibraryPath_RejectsWorldWritable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "libonnxruntime.so")
	writeFile(t, path, 0o666)

	_, err := validateLibraryPath(path)
	if !errors.Is(err, ErrWorldWritable) {
		t.Errorf("got %v, want ErrWorldWritable", err)
	}
}

func TestValidateLibraryPath_RejectsMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := validateLibraryPath(filepath.Join(dir, "does-not-exist.so"))
	if !errors.Is(err, ErrLibraryNotFound) {
		t.Errorf("got %v, want ErrLibraryNotFound", err)
	}
}

func TestResolveLibraryPathWithCandidates_ExplicitWins(t *testing.T) {
	dir := t.TempDir()
	explicitPath := filepath.Join(dir, "explicit.so")
	writeFile(t, explicitPath, 0o644)
	envPath := filepath.Join(dir, "env.so")
	writeFile(t, envPath, 0o644)

	got, err := resolveLibraryPathWithCandidates(explicitPath, envPath, nil)
	if err != nil {
		t.Fatalf("resolveLibraryPathWithCandidates: %v", err)
	}
	if got != explicitPath {
		t.Errorf("got %q, want the explicit path %q, not the env one", got, explicitPath)
	}
}

func TestResolveLibraryPathWithCandidates_ExplicitInvalidFailsImmediately(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.so")
	writeFile(t, envPath, 0o644)

	// The explicit path is relative — must fail with ErrRelativePath,
	// never silently fall through to the (valid) env path.
	_, err := resolveLibraryPathWithCandidates("relative.so", envPath, nil)
	if !errors.Is(err, ErrRelativePath) {
		t.Errorf("got %v, want ErrRelativePath (must not fall through to $ONNXRUNTIME_LIB_PATH)", err)
	}
}

func TestResolveLibraryPathWithCandidates_EnvWins(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.so")
	writeFile(t, envPath, 0o644)
	standardPath := filepath.Join(dir, "standard.so")
	writeFile(t, standardPath, 0o644)

	got, err := resolveLibraryPathWithCandidates("", envPath, []string{standardPath})
	if err != nil {
		t.Fatalf("resolveLibraryPathWithCandidates: %v", err)
	}
	if got != envPath {
		t.Errorf("got %q, want the env path %q, not a standard location", got, envPath)
	}
}

func TestResolveLibraryPathWithCandidates_FallsBackToStandardLocations(t *testing.T) {
	dir := t.TempDir()
	standardPath := filepath.Join(dir, "standard.so")
	writeFile(t, standardPath, 0o644)

	got, err := resolveLibraryPathWithCandidates("", "", []string{
		filepath.Join(dir, "does-not-exist-1.so"),
		standardPath,
		filepath.Join(dir, "does-not-exist-2.so"),
	})
	if err != nil {
		t.Fatalf("resolveLibraryPathWithCandidates: %v", err)
	}
	if got != standardPath {
		t.Errorf("got %q, want %q (the one standard location that exists)", got, standardPath)
	}
}

func TestResolveLibraryPathWithCandidates_AllSourcesFail(t *testing.T) {
	dir := t.TempDir()
	_, err := resolveLibraryPathWithCandidates("", "", []string{
		filepath.Join(dir, "does-not-exist-1.so"),
		filepath.Join(dir, "does-not-exist-2.so"),
	})
	if !errors.Is(err, ErrLibraryNotFound) {
		t.Errorf("got %v, want ErrLibraryNotFound", err)
	}
	if !strings.Contains(err.Error(), libraryPathEnvVar) {
		t.Errorf("error should name %s so the caller knows how to fix it, got: %v", libraryPathEnvVar, err)
	}
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `CGO_ENABLED=0 go test ./ortcore/... -run 'TestValidateLibraryPath|TestResolveLibraryPathWithCandidates' -v`
Expected: all subtests PASS.

- [ ] **Step 5: `go vet` and `gofmt` check**

Run: `CGO_ENABLED=0 go vet ./ortcore/... && gofmt -l ortcore/`
Expected: no output from either.

- [ ] **Step 6: Commit**

```bash
git add ortcore/errors.go ortcore/resolve.go ortcore/resolve_test.go
git commit -m "feat: ortcore path resolution and safety checks (§3.1, §3.3)"
```

---

### Task 2: `Load`, `checkStatus`, `CreateEnv`, `Close` (`load.go`)

**Files:**
- Modify: `go.mod` (add `github.com/ebitengine/purego v0.10.2`)
- Create: `ortcore/load.go`
- Test: `ortcore/load_test.go`

**Interfaces:**
- Consumes: `resolveLibraryPath(explicit string) (string, error)` and
  the five sentinel errors from Task 1. Consumes, by `RegisterFunc`
  binding, the generated types from J1's `ortcore/ortapi_gen.go`:
  `fnGetApi`, `fnGetVersionString`, `fnCreateEnv`,
  `fnCreateSessionOptions`, `fnCreateSession`, `fnGetErrorMessage`,
  `fnReleaseStatus`, `fnReleaseEnv`, `fnReleaseSessionOptions`, and the
  offset constants `offBaseGetApi`, `offBaseGetVersionString`,
  `offCreateEnv`, `offCreateSessionOptions`, `offCreateSession`,
  `offGetErrorMessage`, `offReleaseStatus`, `offReleaseEnv`,
  `offReleaseSessionOptions`, and `ortAPIVersion`.
- Produces: `type Env struct{...}`, `func Load(opts ...LoadOption)
  (*Env, error)`, `type LoadOption func(*loadOptions)`, `func
  WithLibraryPath(path string) LoadOption`, `func (e *Env) Close()
  error`, and the unexported `checkVersion(reported, pinned string)
  error` — later tasks (J3+) will add fields and methods to `Env`, not
  redefine it.

- [ ] **Step 1: Add the `purego` dependency**

```bash
go get github.com/ebitengine/purego@v0.10.2
```

Expected: `go.mod` gains a `require` block naming
`github.com/ebitengine/purego v0.10.2`, and `go.sum` is created.

- [ ] **Step 2: Write the failing test in `ortcore/load_test.go`**

```go
package ortcore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// cachedLibraryPath locates the locally-cached ONNX Runtime 1.28.0
// shared library (ARQUITETURA_OFICIAL.md §6.7.1) — the same cache
// directory internal/ortgen's tests use for the header. Never
// downloads anything; skips with instructions if absent.
func cachedLibraryPath(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no $HOME to locate the cached library")
	}
	path := filepath.Join(home, ".cache", "goembed", "onnxruntime", "1.28.0", "libonnxruntime.so.1.28.0")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("pinned library not found at %s — download per ARQUITETURA_OFICIAL.md §6.7.1", path)
	}
	return path
}

func TestLoad_Success(t *testing.T) {
	path := cachedLibraryPath(t)

	env, err := Load(WithLibraryPath(path))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer env.Close()

	if env.apiPtr == 0 {
		t.Error("Env.apiPtr is zero after a successful Load")
	}
	if env.env == 0 {
		t.Error("Env.env (OrtEnv handle) is zero after a successful Load")
	}
}

func TestLoad_Close_DoesNotPanicOnDoubleClose(t *testing.T) {
	path := cachedLibraryPath(t)

	env, err := Load(WithLibraryPath(path))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := env.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := env.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestLoad_InvalidPath(t *testing.T) {
	_, err := Load(WithLibraryPath("relative/path.so"))
	if !errors.Is(err, ErrRelativePath) {
		t.Errorf("got %v, want ErrRelativePath — Load must surface resolveLibraryPath's error, not wrap it into something generic", err)
	}
}

func TestCheckVersion_Match(t *testing.T) {
	if err := checkVersion("1.28.0", "1.28.0"); err != nil {
		t.Errorf("checkVersion with matching versions: %v", err)
	}
}

func TestCheckVersion_Mismatch(t *testing.T) {
	err := checkVersion("1.24.3", "1.28.0")
	if !errors.Is(err, ErrVersionMismatch) {
		t.Errorf("got %v, want ErrVersionMismatch", err)
	}
}
```

- [ ] **Step 3: Run the tests to confirm they fail**

Run: `CGO_ENABLED=0 go test ./ortcore/... -run 'TestLoad|TestCheckVersion' -v`
Expected: FAIL — `Load`, `WithLibraryPath`, `checkVersion`, and the
`Env` fields referenced (`apiPtr`, `env`) are undefined.

- [ ] **Step 4: Write `ortcore/load.go`**

```go
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
```

- [ ] **Step 5: Run the tests to confirm they pass**

Run: `CGO_ENABLED=0 go test ./ortcore/... -run 'TestLoad|TestCheckVersion' -v`
Expected: all PASS (or SKIP with a clear message only if
`~/.cache/goembed/onnxruntime/1.28.0/libonnxruntime.so.1.28.0` is
absent — on the machine this plan was written on, it exists, so these
must PASS, not skip).

- [ ] **Step 6: Manually verify the `RegisterFunc`/type pairing against the real library** (closes the §6.8 item-3 gap this task's own header calls out — a wrong Go *type*, as opposed to arity, would only surface here, at runtime, not at `go build`)

Run:
```bash
export ORT_HEADER_DIR=~/.cache/goembed/onnxruntime/1.28.0
CGO_ENABLED=0 go test ./ortcore/... -run TestLoad_Success -v
```
Expected: PASS, and re-read the test's assertions on `env.apiPtr` and
`env.env` being non-zero — this is the concrete evidence that every
`RegisterFunc` binding in `Load` matched its real C function correctly
(a wrong argument type would typically crash the process or hang, not
return a clean Go error, so a clean PASS here is meaningful, not just
"no error returned").

- [ ] **Step 7: Grep-verify the `GetErrorMessage`/`ReleaseStatus` single-call-site invariant**

Run:
```bash
grep -rn "getErrorMessage\|releaseStatus" ortcore/*.go | grep -v _test.go
```
Expected: every match is inside `checkStatus` or its declaration in
`Env`/`Load`'s `RegisterFunc` binding line — no call to
`e.getErrorMessage(...)` or `e.releaseStatus(...)` outside
`checkStatus`.

- [ ] **Step 8: `go vet`, `gofmt`, full module check**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet -unsafeptr=false ./... && CGO_ENABLED=0 go test ./... && gofmt -l .`
Expected: build/vet/test succeed, `gofmt -l .` prints nothing.

(`-unsafeptr=false` is required starting with this task — see the
plan header's "Pre-plan verification" note and `CLAUDE.md` §"Zona de
alto escrutínio" item 6. Without it, plain `go vet` reports 5 false
"possible misuse of unsafe.Pointer" findings on the
`apiPtr+off`/`basePtr+off` dereferences in `load.go` — confirmed while
writing this plan, and confirmed that Task 1's `resolve.go` alone,
before this task exists, passes plain `go vet` with no flag needed at
all.)

- [ ] **Step 9: Commit**

```bash
git add go.mod go.sum ortcore/load.go ortcore/load_test.go
git commit -m "feat: ortcore.Load — dlopen, version check, CreateEnv via RegisterFunc"
```

---

### Task 3: Whole-module verification and diary entry

**Files:**
- Modify: `LOG_DEVELOPMENT.md` (append entry)
- Modify: `ARQUITETURA_OFICIAL.md` (mark J2's §7 row closed — learned
  from J1: do this in the same window, not as a follow-up)

**Interfaces:**
- Consumes: everything from Tasks 1–2.
- Produces: nothing consumed by later tasks — this closes out J2.

- [ ] **Step 1: Run the full module check**

Run:
```bash
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet -unsafeptr=false ./... && CGO_ENABLED=0 go test ./... -v
gofmt -l .
```
Expected: all succeed; `gofmt -l .` prints nothing. This is the literal
J2 row from `ARQUITETURA_OFICIAL.md` §7 ("Carrega a `.so` real, versão
confere, path inválido dá erro tipado").

- [ ] **Step 2: Update `ARQUITETURA_OFICIAL.md` §7's J2 row**

Change:
```
| **J2** | `ortcore`: carga e erros | Carrega a `.so` real, versão confere, path inválido dá erro tipado | §3.3, §5.2 |
```
to the same closed-row pattern used for J0/J1:
```
| **J2** | `ortcore`: carga e erros | ~~Carrega a `.so` real, versão confere, path inválido dá erro tipado~~ **fechada em DATA_REAL** | §3.3, §5.2 |
```
(`DATA_REAL` = the actual date this task runs, not a date copied from
this plan file — J1's fix wave found exactly this kind of staleness
once already; don't repeat it.)

Also bump the document's version/status header and append a §8 history
row summarizing what J2 delivered, in the same style as the J1 entries
(§8's existing rows are the template — read the two most recent ones
before writing the new one).

- [ ] **Step 3: Append a `LOG_DEVELOPMENT.md` entry**

Follow the file's established format (Contexto/Feito/Decisões/
Pendências/Próximo passo — read the file's header for the template).
Cover at minimum: `ortcore.Load`/`WithLibraryPath`/`Close` implemented;
path safety (§3.1/§3.3) fully unit-tested without touching the real
`.so`; `checkStatus` invariant enforced and grep-verified; version
check (§5.4) unit-tested via the extracted `checkVersion` pure
function; checksum verification explicitly deferred (spec marks it
optional); next step is J3 (`ortcore`: sessão e metadados).

- [ ] **Step 4: Commit**

```bash
git add ARQUITETURA_OFICIAL.md LOG_DEVELOPMENT.md
git commit -m "docs: log J2 completion (ortcore.Load)"
```

---

## Self-Review Notes

- **Spec coverage:** §3.1 (discovery order, explicit > env > standard,
  fail-fast on explicit/env) — Task 1. §3.3 (absolute, no `..`,
  canonical via `EvalSymlinks`, not world-writable) — Task 1, each
  check independently tested. §5.4 (version check first, exact match)
  — Task 2, `checkVersion` extracted as a pure function specifically
  so it's testable without dlopen. §3.5/§5.2 (`checkStatus` as the
  sole call site) — Task 2, and mechanically verified by grep in Step
  7, not just asserted in a comment. §2.3 (`GetVersionString` reachable
  via `OrtApiBase` offsets, no `OrtApi` offset needed) — Task 2's
  `Load` reads `offBaseGetVersionString` before ever calling `GetApi`.
- **Deferred to later windows, not a gap here:** `CreateSession`,
  `SessionGetInputName`/`OutputName`, tensors, and `Run` are J3/J4
  scope per `ARQUITETURA_OFICIAL.md` §7 — `Env.createSessionOptions`
  and `Env.createSession` are bound in `Load` (since they share the
  same `apiPtr` setup work) but never called by this plan; J3 calls
  them. `runtime.Pinner` (§3.6) is explicitly out of scope until a
  buffer is actually retained across a call boundary (J4, tensors) —
  `cString`'s buffer is not retained by ORT past `CreateEnv`
  returning, reasoned about explicitly in `load.go`'s doc comment
  rather than left silent.
- **Type consistency:** every `fnX` name and offset constant this plan
  references (`fnCreateEnv`, `fnCreateSessionOptions`,
  `fnCreateSession`, `fnGetErrorMessage`, `fnReleaseStatus`,
  `fnReleaseEnv`, `fnReleaseSessionOptions`, `fnGetApi`,
  `fnGetVersionString`, and all nine offset constants) was checked
  against the actual, current `ortcore/ortapi_gen.go` — copied
  verbatim, not retyped from memory — and exercised against the real
  `.so` in the pre-plan spike before this plan was written.
