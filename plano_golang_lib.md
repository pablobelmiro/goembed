# Épico 1 — Fronteira FFI purego/ONNX Runtime: correções e esqueleto

## 1. Correção crítica ao plano anterior

Na rodada passada eu descrevi `purego.RegisterFunc` sendo chamado função a função
(`CreateEnv`, `CreateSession` etc.) como se cada uma fosse um símbolo exportado
da `.so`/`.dll`. **Isso está errado e precisa ser corrigido antes de você
escrever qualquer linha de código**, porque senão o primeiro passo do steel
thread já nasce sobre uma premissa falsa.

A ONNX Runtime C API não exporta essas funções individualmente. O único
símbolo exportado é `OrtGetApiBase`. O fluxo real é:

```
OrtGetApiBase()              → símbolo exportado, pegável via dlsym/purego.RegisterFunc
    → *OrtApiBase             → struct com 1 função: GetApi(uint32_t version)
        → GetApi(ORT_API_VERSION) → *OrtApi
            → OrtApi é uma struct de ~250 ponteiros de função, em uma ordem
              fixa definida pelo header onnxruntime_c_api.h daquela versão
```

Ou seja: `CreateEnv`, `CreateSession`, `Run`, `GetErrorMessage` etc. **não são
símbolos**, são campos (offsets) dentro dessa struct `OrtApi`. Você não vai
usar `purego.RegisterFunc` para eles — vai usar `purego.SyscallN` apontando
para o ponteiro de função lido naquele offset da struct.

Isso muda a natureza do risco do Épico 1: o perigo não é mais só "inverter um
ponteiro simples por um duplo" — é **acertar a posição de cada campo na
struct**, porque a ordem no header é a única fonte de verdade e ela muda entre
versões do ONNX Runtime. Errar um offset não dá erro de compilação nem de
link: você simplesmente chama a função errada da tabela, com os argumentos
errados. É o mesmo tipo de corrupção silenciosa que você já esperava, só que
uma camada abaixo de onde eu descrevi antes.

### Referência de mercado

O `yalue/onnxruntime_go` resolve isso com um arquivo C intermediário
(`onnxruntime_wrapper.c`) compilado via cgo, que faz o dereference da struct
do lado do C, onde o compilador C garante a ordem certa. Isso é exatamente o
que seu projeto está evitando ao propor zero-CGO — então você não tem esse
colchão de segurança e precisa substituí-lo por outro mecanismo.

## 2. O mecanismo de segurança: gerar o offset table, não digitar à mão

Não digite offsets manualmente. Gere-os automaticamente contra o header
pinado, uma única vez por versão do ONNX Runtime que você suportar.

Passo de desenvolvimento (não entra no binário final, não quebra o zero-CGO
em runtime — roda só via `go generate`, uma vez, localmente):

**`internal/ortgen/dump_offsets.c`** (não compilado no build normal do
pacote — vive fora do módulo Go principal, só usado para gerar o arquivo
`.go` abaixo)

```c
// Compilar com: gcc -I<pasta com onnxruntime_c_api.h vX.Y.Z> dump_offsets.c -o dump_offsets
#include <stdio.h>
#include <stddef.h>
#include "onnxruntime_c_api.h"

int main(void) {
    printf("// Código gerado por dump_offsets.c contra onnxruntime_c_api.h — NÃO EDITAR À MÃO\n");
    printf("// Versão ORT_API_VERSION: %d\n\n", ORT_API_VERSION);
    printf("const (\n");
    printf("\toffCreateEnv          = %zu\n", offsetof(OrtApi, CreateEnv));
    printf("\toffCreateSessionOpts  = %zu\n", offsetof(OrtApi, CreateSessionOptions));
    printf("\toffCreateSession      = %zu\n", offsetof(OrtApi, CreateSession));
    printf("\toffRun                = %zu\n", offsetof(OrtApi, Run));
    printf("\toffGetErrorMessage    = %zu\n", offsetof(OrtApi, GetErrorMessage));
    printf("\toffReleaseStatus      = %zu\n", offsetof(OrtApi, ReleaseStatus));
    printf("\toffReleaseEnv         = %zu\n", offsetof(OrtApi, ReleaseEnv));
    printf("\toffReleaseSession     = %zu\n", offsetof(OrtApi, ReleaseSession));
    printf(")\n");
    return 0;
}
```

Rode isso uma vez por versão suportada, redirecione a saída para
`ortapi_offsets_gen.go`, e comite o `.go` gerado (não o binário `dump_offsets`,
não o header). O arquivo `.c` fica documentado no repo como a fonte de
verdade, mas nunca entra no pipeline de build do pacote Go — só é reexecutado
manualmente quando você adicionar suporte a uma nova versão do ORT. Isso
resolve exatamente o mesmo tipo de isolamento que você já tinha proposto para
o modelo sintético (Seção 16.3): a dependência externa (aqui, o compilador C e
o header oficial) fica confinada a uma ferramenta de geração, fora do produto
final.

## 3. Esqueleto Go do steel thread

```go
// ortapi.go — núcleo da fronteira FFI. Superfície mínima, alto escrutínio.
// Qualquer alteração aqui exige reconferência manual contra o header oficial.
package ortcore

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

type api struct {
	lib     uintptr
	apiPtr  uintptr // *OrtApi — base para todos os offsets gerados
}

var (
	ortGetApiBase func() uintptr
)

func Load(libPath string) (*api, error) {
	lib, err := purego.Dlopen(libPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("dlopen %s: %w", libPath, err)
	}

	purego.RegisterLibFunc(&ortGetApiBase, lib, "OrtGetApiBase")

	apiBasePtr := ortGetApiBase() // *OrtApiBase
	// OrtApiBase{ GetApi func(uint32) *OrtApi; GetVersionString func() *char }
	// GetApi é o 1º campo (offset 0) — isso é estável entre versões,
	// só a struct OrtApi em si muda de layout.
	getApiFn := *(*uintptr)(unsafe.Pointer(apiBasePtr))

	const ortAPIVersion = 20 // ajustar para a versão pinada do seu .so
	apiPtr, _, _ := purego.SyscallN(getApiFn, uintptr(ortAPIVersion))
	if apiPtr == 0 {
		return nil, fmt.Errorf("GetApi retornou nil para ORT_API_VERSION=%d — versão do .so incompatível com os offsets gerados", ortAPIVersion)
	}

	return &api{lib: lib, apiPtr: apiPtr}, nil
}

// fnAt lê o ponteiro de função no offset dado dentro da struct OrtApi.
func (a *api) fnAt(offset uintptr) uintptr {
	return *(*uintptr)(unsafe.Pointer(a.apiPtr + offset))
}

// --- Sanity check: rode isto ANTES de qualquer outra coisa ---
// Confirma que os offsets gerados batem com o .so carregado em runtime,
// sem depender de nenhuma função "de negócio" ainda.
func (a *api) VerifyOffsets() error {
	// GetVersionString não depende de nenhum objeto criado — é o teste
	// mais barato de "os offsets fazem sentido".
	// (offset gerado por dump_offsets.c — ver ortapi_offsets_gen.go)
	ptr, _, _ := purego.SyscallN(a.fnAt(offGetVersionString))
	if ptr == 0 {
		return fmt.Errorf("GetVersionString retornou nil — offsets desalinhados com o .so carregado")
	}
	got := goStringFromC(ptr)
	fmt.Printf("ORT runtime reporta versão: %s\n", got)
	// compare 'got' contra a versão que você pinou no go.mod / README
	return nil
}
```

```go
// ortstrings.go — cópia segura de strings C, sem strlen do stdlib
package ortcore

import "unsafe"

// goStringFromC varre byte a byte até \0. purego não dá tamanho, só ponteiro.
func goStringFromC(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	n := 0
	for {
		b := *(*byte)(unsafe.Pointer(ptr + uintptr(n)))
		if b == 0 {
			break
		}
		n++
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n)
	out := make([]byte, n)
	copy(out, buf) // cópia real — buf aponta pra memória que o ORT ainda possui
	return string(out)
}
```

```go
// errors.go — ownership de OrtStatus centralizado num só lugar
package ortcore

import "fmt"

// checkStatus copia a mensagem de erro (se houver) e libera o OrtStatus
// imediatamente. Nenhum outro ponto do pacote deve chamar GetErrorMessage
// ou ReleaseStatus diretamente — só esta função.
func (a *api) checkStatus(statusPtr uintptr) error {
	if statusPtr == 0 {
		return nil // sucesso
	}
	msgPtr, _, _ := purego.SyscallN(a.fnAt(offGetErrorMessage), statusPtr)
	msg := goStringFromC(msgPtr) // cópia ANTES de liberar o status
	purego.SyscallN(a.fnAt(offReleaseStatus), statusPtr)
	return fmt.Errorf("ort: %s", msg)
}
```

Repare que `CreateEnv`, `CreateSessionOptions`, `CreateSession` e `Run` ainda
não estão implementadas de propósito — a tabela de offsets (`ortapi_offsets_gen.go`)
é o único artefato que falta, e ele sai do passo 2 rodando contra o header
real que você vai pinar. Eu não vou inventar esses números aqui: um offset
errado digitado por mim tem exatamente o mesmo efeito destrutivo que um
gerado por um agente sem revisão, e nesse ponto específico da fronteira não
vale a pena economizar o passo de geração automática.

## 4. Ordem de execução recomendada para o Épico 1

1. Pinar a versão exata do `onnxruntime_c_api.h` e do `.so`/`.dylib` que o
   projeto vai suportar no v1.0 (uma só, não generalizar ainda).
2. Rodar `dump_offsets.c` contra esse header → gerar `ortapi_offsets_gen.go`.
3. Colar o esqueleto acima, compilar, rodar `VerifyOffsets()` isolado.
4. Só depois de `VerifyOffsets()` imprimir a versão certa, implementar
   `CreateEnv → CreateSessionOptions → CreateSession → Run → ReleaseSession → ReleaseEnv`
   um de cada vez, cada um testado contra o modelo sintético de adição
   (Seção 16.3) antes de passar para o próximo.
5. Só depois disso o Claude Code entra, para a parte de largura.

## 5. Prompt sugerido para o Claude Code (trabalho de largura, pós steel thread)

```
Contexto: pacote Go `ortcore` (zero-CGO, purego) para ONNX Runtime. O núcleo
da fronteira FFI (ortapi.go, ortstrings.go, errors.go, ortapi_offsets_gen.go)
já existe, foi validado manualmente contra o modelo sintético de teste em
testdata/, e é considerado estável. NÃO modifique esses quatro arquivos nem
os offsets neles contidos sob nenhuma circunstância — se você achar que um
offset está errado, pare e me avise, não corrija por conta própria.

Sua tarefa é construir a API pública idiomática por cima desse núcleo:

1. Wrappers Go idiomáticos (CreateEnv, NewSession, session.Run etc.) que
   escondem os detalhes de ponteiro cru e uintptr do pacote ortcore, expondo
   apenas tipos Go seguros (structs, slices, error).
2. Cada wrapper deve chamar checkStatus() do núcleo para tratamento de erro
   — não implemente tratamento de erro paralelo.
3. Testes de tabela para os casos de sucesso e de erro de cada wrapper,
   usando o modelo sintético em testdata/ (não crie novos modelos de teste;
   se precisar de um cenário que o modelo atual não cobre, pare e me avise).
4. Documentação em formato godoc para cada símbolo exportado.
5. Sem dependências de terceiros além do que já está em go.mod — stdlib
   ou o próprio purego, nada além disso.

Ao final, rode `go vet` e a suíte de testes completa, e liste explicitamente
quais arquivos você tocou.
```

Esse prompt fixa a fronteira como zona proibida, dá ao agente uma tarefa com
raio de erro pequeno (wrappers e testes, detectáveis por `go test`), e ainda
assim aproveita a velocidade dele onde ela não é um risco.
