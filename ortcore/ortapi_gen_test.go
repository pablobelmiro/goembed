package ortcore

import "testing"

func TestOrtAPIVersionIsPinned(t *testing.T) {
	const want = 28 // ARQUITETURA_OFICIAL.md §6.7 — ONNX Runtime 1.28.0
	if ortAPIVersion != want {
		t.Fatalf("ortAPIVersion = %d, want %d", ortAPIVersion, want)
	}
}

func TestSteelThreadOffsets(t *testing.T) {
	// Verified against the real onnxruntime_c_api.h v1.28.0 in
	// ARQUITETURA_OFICIAL.md §2.2 and §6.7.1. If any of these change
	// after a regeneration, treat it as a signal to re-read §6.7.1
	// before assuming it's fine.
	cases := map[string]uintptr{
		"offBaseGetApi":            0,
		"offBaseGetVersionString":  8,
		"offCreateEnv":             24,
		"offCreateSessionOptions":  80,
		"offCreateSession":         56,
		"offRun":                   72,
		"offGetErrorCode":          8,
		"offGetErrorMessage":       16,
		"offReleaseStatus":         744,
	}
	got := map[string]uintptr{
		"offBaseGetApi":           offBaseGetApi,
		"offBaseGetVersionString": offBaseGetVersionString,
		"offCreateEnv":            offCreateEnv,
		"offCreateSessionOptions": offCreateSessionOptions,
		"offCreateSession":        offCreateSession,
		"offRun":                  offRun,
		"offGetErrorCode":         offGetErrorCode,
		"offGetErrorMessage":      offGetErrorMessage,
		"offReleaseStatus":        offReleaseStatus,
	}
	for name, want := range cases {
		if got[name] != want {
			t.Errorf("%s = %d, want %d", name, got[name], want)
		}
	}
}
