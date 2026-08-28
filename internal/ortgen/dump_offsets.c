//go:build ignore
//
// Este arquivo é embutido via //go:embed em main.go e compilado para um
// binário independente em tempo de geração — nunca faz parte do build do
// pacote Go em si. A tag de build acima é uma defesa: mantém
// internal/ortgen buildável mesmo que CGO_ENABLED=1 vaze do ambiente. Sem
// ela, `go build`/`go vet`/`go test` já funcionam sob CGO_ENABLED=0 (o
// modo que este projeto sempre usa) com ou sem a tag — mas falhariam sob
// CGO_ENABLED=1 com "C source files not allowed when not using cgo or
// SWIG", porque o `go` tool trataria este .c solto (sem `import "C"`)
// como erro. A tag é reforço opcional, não correção de um defeito.

// dump_offsets.c gera, via `go generate` (internal/ortgen), as
// constantes de offset e os tipos de função Go que espelham a struct
// OrtApi do ONNX Runtime pinado (ARQUITETURA_OFICIAL.md §6.7).
//
// Cada entrada de ORT_FUNCTIONS declara, numa única linha-fonte, o
// offset E o tipo Go equivalente a partir da MESMA assinatura C. A
// macro CHECK abaixo verifica essa assinatura C contra o header real em
// TEMPO DE COMPILAÇÃO deste próprio arquivo — se um campo de OrtApi
// mudar de assinatura numa versão futura do ORT, a compilação deste
// arquivo falha com uma mensagem nomeando o campo, em vez de produzir
// uma tabela de offsets silenciosamente incorreta.
//
// Nunca edite ortcore/ortapi_gen.go à mão — edite este arquivo e rode
// `go generate ./...` (ver ortcore/generate.go e CLAUDE.md).
#include <stdio.h>
#include <stddef.h>
#include "onnxruntime_c_api.h"

#define ORT_FUNCTIONS(X) \
  X(CreateEnv, OrtStatus*(*)(OrtLoggingLevel, const char*, OrtEnv**), \
    "func(logSeverityLevel int32, logid uintptr, out *uintptr) uintptr") \
  X(CreateSessionOptions, OrtStatus*(*)(OrtSessionOptions**), \
    "func(out *uintptr) uintptr") \
  X(CreateSession, OrtStatus*(*)(const OrtEnv*, const ORTCHAR_T*, const OrtSessionOptions*, OrtSession**), \
    "func(env uintptr, modelPath uintptr, options uintptr, out *uintptr) uintptr") \
  X(Run, OrtStatus*(*)(OrtSession*, const OrtRunOptions*, const char* const*, const OrtValue* const*, size_t, const char* const*, size_t, OrtValue**), \
    "func(session uintptr, runOptions uintptr, inputNames uintptr, inputs uintptr, inputLen uintptr, outputNames uintptr, outputNamesLen uintptr, outputs uintptr) uintptr") \
  X(GetErrorCode, OrtErrorCode(*)(const OrtStatus*), \
    "func(status uintptr) int32") \
  X(GetErrorMessage, const char*(*)(const OrtStatus*), \
    "func(status uintptr) uintptr") \
  X(ReleaseStatus, void(*)(OrtStatus*), \
    "func(input uintptr)") \
  X(CreateCpuMemoryInfo, OrtStatus*(*)(OrtAllocatorType, OrtMemType, OrtMemoryInfo**), \
    "func(typ int32, memType int32, out *uintptr) uintptr") \
  X(CreateTensorWithDataAsOrtValue, OrtStatus*(*)(const OrtMemoryInfo*, void*, size_t, const int64_t*, size_t, ONNXTensorElementDataType, OrtValue**), \
    "func(info uintptr, pData uintptr, pDataLen uintptr, shape uintptr, shapeLen uintptr, typ int32, out *uintptr) uintptr") \
  X(GetTensorMutableData, OrtStatus*(*)(OrtValue*, void**), \
    "func(value uintptr, out *uintptr) uintptr") \
  X(GetTensorTypeAndShape, OrtStatus*(*)(const OrtValue*, OrtTensorTypeAndShapeInfo**), \
    "func(value uintptr, out *uintptr) uintptr") \
  X(GetDimensionsCount, OrtStatus*(*)(const OrtTensorTypeAndShapeInfo*, size_t*), \
    "func(info uintptr, out *uintptr) uintptr") \
  X(GetDimensions, OrtStatus*(*)(const OrtTensorTypeAndShapeInfo*, int64_t*, size_t), \
    "func(info uintptr, dimValues uintptr, dimValuesLength uintptr) uintptr") \
  X(SessionGetInputCount, OrtStatus*(*)(const OrtSession*, size_t*), \
    "func(session uintptr, out *uintptr) uintptr") \
  X(SessionGetOutputCount, OrtStatus*(*)(const OrtSession*, size_t*), \
    "func(session uintptr, out *uintptr) uintptr") \
  X(SessionGetInputName, OrtStatus*(*)(const OrtSession*, size_t, OrtAllocator*, char**), \
    "func(session uintptr, index uintptr, allocator uintptr, value *uintptr) uintptr") \
  X(SessionGetOutputName, OrtStatus*(*)(const OrtSession*, size_t, OrtAllocator*, char**), \
    "func(session uintptr, index uintptr, allocator uintptr, value *uintptr) uintptr") \
  X(AllocatorFree, OrtStatus*(*)(OrtAllocator*, void*), \
    "func(allocator uintptr, p uintptr) uintptr") \
  X(GetAllocatorWithDefaultOptions, OrtStatus*(*)(OrtAllocator**), \
    "func(out *uintptr) uintptr") \
  X(ReleaseEnv, void(*)(OrtEnv*), \
    "func(input uintptr)") \
  X(ReleaseSession, void(*)(OrtSession*), \
    "func(input uintptr)") \
  X(ReleaseSessionOptions, void(*)(OrtSessionOptions*), \
    "func(input uintptr)") \
  X(ReleaseValue, void(*)(OrtValue*), \
    "func(input uintptr)") \
  X(ReleaseMemoryInfo, void(*)(OrtMemoryInfo*), \
    "func(input uintptr)") \
  X(ReleaseTensorTypeAndShapeInfo, void(*)(OrtTensorTypeAndShapeInfo*), \
    "func(input uintptr)")

// Verificação de assinatura em tempo de COMPILAÇÃO — nunca executada.
// __typeof__ não avalia seu operando (mesma garantia de sizeof), então
// ((OrtApi*)0)->NAME nunca desreferencia ponteiro nulo em tempo de
// execução.
#define CHECK(NAME, CSIG, GOSIG) \
  _Static_assert(__builtin_types_compatible_p(__typeof__(((OrtApi*)0)->NAME), CSIG), \
                 "OrtApi." #NAME " diverge do header pinado");
ORT_FUNCTIONS(CHECK)

// Idem para os dois campos de OrtApiBase (GetApi e GetVersionString não
// fazem parte de OrtApi — ver ARQUITETURA_OFICIAL.md §2.3).
_Static_assert(__builtin_types_compatible_p(
    __typeof__(((OrtApiBase*)0)->GetApi), const OrtApi*(*)(uint32_t)),
    "OrtApiBase.GetApi diverge do header pinado");
_Static_assert(__builtin_types_compatible_p(
    __typeof__(((OrtApiBase*)0)->GetVersionString), const char*(*)(void)),
    "OrtApiBase.GetVersionString diverge do header pinado");

int main(void) {
    printf("const ortAPIVersion = %d\n\n", ORT_API_VERSION);

    printf("const offBaseGetApi = %zu\n", offsetof(OrtApiBase, GetApi));
    printf("const offBaseGetVersionString = %zu\n", offsetof(OrtApiBase, GetVersionString));
#define PRINT_OFFSET(NAME, CSIG, GOSIG) \
    printf("const off%s = %zu\n", #NAME, offsetof(OrtApi, NAME));
    ORT_FUNCTIONS(PRINT_OFFSET)
    printf("\n");

    printf("// C: const OrtApi*(*)(uint32_t)\n");
    printf("type fnGetApi func(version uint32) uintptr\n");
    printf("// C: const char*(*)(void)\n");
    printf("type fnGetVersionString func() uintptr\n");
// PRINT_TYPE emite, para cada função, um comentário com a assinatura C
// (estringizada via #CSIG — os parênteses internos da assinatura já
// protegem suas vírgulas de serem lidas como separadores de argumento
// da macro, então isso é seguro) imediatamente acima do tipo Go gerado.
// Isso dá a internal/ortgen/main.go um par C/Go, linha a linha, contra o
// qual conferir a aridade — sem esse comentário, GOSIG é uma string
// digitada à mão sem nenhuma verificação (ver ARQUITETURA_OFICIAL.md,
// achado Important 1 da revisão final da J1).
#define PRINT_TYPE(NAME, CSIG, GOSIG) \
    printf("// C: %s\n", #CSIG); \
    printf("type fn%s %s\n", #NAME, GOSIG);
    ORT_FUNCTIONS(PRINT_TYPE)

    return 0;
}
