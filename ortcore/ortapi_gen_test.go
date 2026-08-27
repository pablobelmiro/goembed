package ortcore

import "testing"

func TestOrtAPIVersionIsPinned(t *testing.T) {
	const want = 28 // ARQUITETURA_OFICIAL.md §6.7 — ONNX Runtime 1.28.0
	if ortAPIVersion != want {
		t.Fatalf("ortAPIVersion = %d, want %d", ortAPIVersion, want)
	}
}

// TestSteelThreadOffsets is a table of {name, got, want} rather than the
// two parallel maps this used to be. Two maps invited a class of mistake
// where a typo'd key in one map silently created a spurious pair (or
// dropped a case) instead of failing the offset it meant to check — a
// key-typo bug would present as a "wrong offset", not as itself. A
// struct literal makes got/want adjacent and keeps the field name
// attached to the specific value it names.
//
// Covers every offset the generator currently emits (verified against
// the real onnxruntime_c_api.h v1.28.0 in ARQUITETURA_OFICIAL.md §2.2
// and §6.7.1), not just the original steel-thread subset — the
// generator already produces the whole table, so checking all of it
// costs nothing extra. If any of these change after a regeneration,
// treat it as a signal to re-read §6.7.1 before assuming it's fine.
func TestSteelThreadOffsets(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"offBaseGetApi", offBaseGetApi, 0},
		{"offBaseGetVersionString", offBaseGetVersionString, 8},
		{"offCreateEnv", offCreateEnv, 24},
		{"offCreateSessionOptions", offCreateSessionOptions, 80},
		{"offCreateSession", offCreateSession, 56},
		{"offRun", offRun, 72},
		{"offGetErrorCode", offGetErrorCode, 8},
		{"offGetErrorMessage", offGetErrorMessage, 16},
		{"offReleaseStatus", offReleaseStatus, 744},
		{"offCreateCpuMemoryInfo", offCreateCpuMemoryInfo, 552},
		{"offCreateTensorWithDataAsOrtValue", offCreateTensorWithDataAsOrtValue, 392},
		{"offGetTensorMutableData", offGetTensorMutableData, 408},
		{"offGetTensorTypeAndShape", offGetTensorTypeAndShape, 520},
		{"offGetDimensionsCount", offGetDimensionsCount, 488},
		{"offGetDimensions", offGetDimensions, 496},
		{"offSessionGetInputCount", offSessionGetInputCount, 240},
		{"offSessionGetOutputCount", offSessionGetOutputCount, 248},
		{"offSessionGetInputName", offSessionGetInputName, 288},
		{"offSessionGetOutputName", offSessionGetOutputName, 296},
		{"offAllocatorFree", offAllocatorFree, 608},
		{"offGetAllocatorWithDefaultOptions", offGetAllocatorWithDefaultOptions, 624},
		{"offReleaseEnv", offReleaseEnv, 736},
		{"offReleaseSession", offReleaseSession, 760},
		{"offReleaseSessionOptions", offReleaseSessionOptions, 800},
		{"offReleaseValue", offReleaseValue, 768},
		{"offReleaseMemoryInfo", offReleaseMemoryInfo, 752},
		{"offReleaseTensorTypeAndShapeInfo", offReleaseTensorTypeAndShapeInfo, 792},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}
