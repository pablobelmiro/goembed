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
