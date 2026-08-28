package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cachedHeaderDir locates the locally-cached ONNX Runtime 1.28.0 header
// directory (ARQUITETURA_OFICIAL.md §6.7.1). It never downloads
// anything — if the cache is absent, the test skips with instructions.
func cachedHeaderDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("ORT_HEADER_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no $HOME to locate the cached header, and ORT_HEADER_DIR is unset")
	}
	dir := filepath.Join(home, ".cache", "goembed", "onnxruntime", "1.28.0")
	if _, err := os.Stat(filepath.Join(dir, "onnxruntime_c_api.h")); err != nil {
		t.Skipf("pinned header not found at %s — download per ARQUITETURA_OFICIAL.md §6.7.1, or set ORT_HEADER_DIR", dir)
	}
	return dir
}

func TestGenerate_AgainstRealHeader(t *testing.T) {
	dir := cachedHeaderDir(t)

	out, err := generate(dir, dumpOffsetsC)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src := string(out)

	if !strings.Contains(src, "package ortcore") {
		t.Fatal("output does not declare package ortcore")
	}
	if !strings.Contains(src, "ortAPIVersion = 28") {
		t.Error("expected ortAPIVersion = 28 (ORT pinned in ARQUITETURA_OFICIAL.md §6.7)")
	}
	if !strings.Contains(src, "offCreateEnv = 24") {
		t.Error("expected offCreateEnv = 24 (verified in ARQUITETURA_OFICIAL.md §2.2)")
	}
	if !strings.Contains(src, "offRun = 72") {
		t.Error("expected offRun = 72")
	}
	if got := strings.Count(src, "type fn"); got != 27 {
		t.Errorf("expected 27 type declarations (25 OrtApi + 2 OrtApiBase), got %d", got)
	}
	if got := strings.Count(src, "// C: "); got != 27 {
		t.Errorf("expected 27 \"// C: \" signature comments (one per generated type, ARQUITETURA_OFICIAL.md §6.8 item 1 — every type must be arity-checked), got %d", got)
	}
}

func TestGenerate_MissingSignatureCommentFailsArityCheck(t *testing.T) {
	dir := cachedHeaderDir(t)

	// Reproduces the exact regression ARQUITETURA_OFICIAL.md §6.8 item 1
	// warned about: dump_offsets.c's PRINT_TYPE macro stops emitting the
	// "// C: ..." comment above generated types. Before this test existed,
	// checkGoSignatureArity would simply find nothing to check and return
	// nil — silently leaving every type unguarded again, reopening the
	// hole the final branch review found and this whole mechanism exists
	// to close. It must now fail loudly, because the count of "// C: "
	// comments no longer matches the count of "type fn" declarations.
	const commentPrintf = `    printf("// C: %s\n", #CSIG); \` + "\n"
	brokenSrc := strings.Replace(dumpOffsetsC, commentPrintf, "", 1)
	if brokenSrc == dumpOffsetsC {
		t.Fatal("replacement did not match PRINT_TYPE's comment-emitting line — did dump_offsets.c change?")
	}

	_, err := generate(dir, brokenSrc)
	if err == nil {
		t.Fatal("expected an error when no \"// C: \" comments are emitted at all, generate() succeeded")
	}
	if !strings.Contains(err.Error(), "were paired with a") {
		t.Errorf("error does not mention the unguarded-count mismatch, got: %v", err)
	}
}

func TestGenerate_DivergentGoSignatureFailsArityCheck(t *testing.T) {
	dir := cachedHeaderDir(t)

	// Deliberately drop one parameter from CreateSession's hand-typed Go
	// signature (GOSIG) while leaving its C signature (CSIG) untouched.
	// dump_offsets.c's CHECK macro only verifies CSIG against the real
	// header — nothing previously verified GOSIG against CSIG, so this
	// used to compile and generate without any error at all (the exact
	// hole the final branch review demonstrated). It must now be caught
	// by checkGoSignatureArity in generate(), which compares the "// C:
	// ..." comment PRINT_TYPE emits against the paired Go type.
	const original = `"func(env uintptr, modelPath uintptr, options uintptr, out *uintptr) uintptr") \`
	const broken = `"func(env uintptr, modelPath uintptr, out *uintptr) uintptr") \`
	brokenSrc := strings.Replace(dumpOffsetsC, original, broken, 1)
	if brokenSrc == dumpOffsetsC {
		t.Fatal("replacement did not match CreateSession's GOSIG line — did dump_offsets.c change?")
	}

	_, err := generate(dir, brokenSrc)
	if err == nil {
		t.Fatal("expected an arity-mismatch error for the divergent GOSIG, generate() succeeded")
	}
	if !strings.Contains(err.Error(), "fnCreateSession") {
		t.Errorf("error does not mention fnCreateSession, got: %v", err)
	}
}

func TestGenerate_DivergentSignatureFailsToCompile(t *testing.T) {
	dir := cachedHeaderDir(t)

	// Deliberately drop one argument from CreateSession's declared C
	// signature. This must fail at dump_offsets.c's OWN compilation
	// (caught by CHECK's _Static_assert), not at runtime — that is
	// exactly what ARQUITETURA_OFICIAL.md §6.1 promises.
	const original = `X(CreateSession, OrtStatus*(*)(const OrtEnv*, const ORTCHAR_T*, const OrtSessionOptions*, OrtSession**),`
	const broken = `X(CreateSession, OrtStatus*(*)(const OrtEnv*, const ORTCHAR_T*, OrtSession**),`
	brokenSrc := strings.Replace(dumpOffsetsC, original, broken, 1)
	if brokenSrc == dumpOffsetsC {
		t.Fatal("replacement did not match the CreateSession line — did dump_offsets.c change?")
	}

	_, err := generate(dir, brokenSrc)
	if err == nil {
		t.Fatal("expected a compile failure for the divergent signature, generate() succeeded")
	}
	if !strings.Contains(err.Error(), "CreateSession") {
		t.Errorf("error does not mention CreateSession, got: %v", err)
	}
}
