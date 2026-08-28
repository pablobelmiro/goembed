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
