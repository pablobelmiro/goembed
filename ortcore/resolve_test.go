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
