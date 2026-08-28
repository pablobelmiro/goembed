# ARQUITETURA OFICIAL

> **Status:** v0.9 — J0, J1 e J2 fechadas e revisadas; ortcore.Load,
> WithLibraryPath e Close implementados e verificados.
> Fonte única de verdade do projeto.
> **Última atualização:** 2026-08-28
>
> Como ler: tudo aqui está rotulado como **[VERIFICADO]** (testado nesta
> máquina, com método e data), **[DECIDIDO]** (escolha feita, com
> justificativa, revogável) ou **[EM ABERTO]** (ainda não resolvido).
> Afirmação sem rótulo é erro de manutenção deste documento — aponte.

---

## 1. Produto

Biblioteca Go para **inferência de modelos transformer**, entregando
pipelines de alto nível no estilo `transformers` do HuggingFace, com a
cadeia de build **inteiramente livre de CGO**.

- **Público:** desenvolvedores Go que precisam de ML em produção sem
  manter um serviço Python ao lado.
- **Caso de uso do v1.0:** *embeddings de texto* (`feature-extraction`) —
  texto → vetor. Alimenta busca semântica, RAG e deduplicação, que é a
  demanda dominante da comunidade Go hoje. **[DECIDIDO]**
- **Critério de v1.0:** publicado e utilizável por terceiros sem contato
  com o autor — README, CI, godoc, SemVer. **[DECIDIDO]**

### 1.1 O diferencial **[REVISADO — v0.3, ver §2.6]**

> **REVOGADO (v0.1–v0.2):** *"O valor é `CGO_ENABLED=0` de ponta a ponta:
> cross-compile trivial, nenhum toolchain C na máquina de build, binário
> Go estático."*
>
> Duas premissas dessa formulação são **factualmente falsas** (§2.6):
> o `hugot` já entrega zero-CGO no build padrão, e `CGO_ENABLED=0` com
> purego **não** produz binário estático — o binário continua ligado
> dinamicamente a `libdl.so.2`.

**Formulação corrigida:**

> **Velocidade nativa do ONNX Runtime, sem CGO em tempo de build.**

Hoje é preciso escolher um dos dois; ninguém entrega os dois juntos:

| Opção existente | CGO no build | Motor de cálculo | Velocidade |
|---|---|---|---|
| `hugot` padrão (backend GO) | **não** | SimpleGo (GoMLX) | **5–20× mais lento** |
| `hugot` com tag `ORT` | **sim** | ONNX Runtime nativo | nativa |
| `yalue/onnxruntime_go` | **sim** | ONNX Runtime nativo | nativa |
| **este projeto** | **não** | ONNX Runtime nativo | nativa |

**Corolário que governa todo o resto:** o diferencial continua *frágil e
indivisível* — se qualquer elo da cadeia exigir CGO, ele vale zero. Mas o
que se ganha ao preservá-lo é mais modesto do que se afirmava: **não é
binário estático, é ausência de toolchain C no build e cross-compile sem
sysroot.** A `.so` do ORT continua sendo requisito de runtime (§3.1), e
`libdl.so.2` também.

---

## 2. Fatos verificados

Verificados em **2026-08-26**, nesta máquina (Ubuntu, linux/amd64,
Go 1.26.2, gcc 13.3), contra `libonnxruntime.so.1.24.3`.

### 2.1 A ONNX Runtime C API expõe dois símbolos **[VERIFICADO]**

```
$ nm -D --defined-only libonnxruntime.so.1.24.3 | grep -c " T "
2
```

Apenas `OrtGetApiBase` e `OrtSessionOptionsAppendExecutionProvider_CPU`.
`CreateSession`, `Run`, `GetErrorMessage` **não são símbolos** — são
campos da struct `OrtApi`. Confirma a Seção 1 do `plano_golang_lib.md`.

### 2.2 Geometria da `OrtApi` **[VERIFICADO]**

Contra `onnxruntime_c_api.h` da tag `v1.24.3` (mais o
`onnxruntime_ep_c_api.h`, que ele inclui — **o header não é autocontido**):

| Propriedade | Valor |
|---|---|
| `ORT_API_VERSION` | **24** |
| `sizeof(OrtApi)` | 3320 bytes |
| Ponteiros de função | **415** |

> **Correção ao plano anterior:** `plano_golang_lib.md:125` fixa
> `ortAPIVersion = 20`. O valor correto para o `.so` desta máquina é **24**.

### 2.3 `GetVersionString` está em `OrtApiBase`, não em `OrtApi` **[VERIFICADO]**

```c
struct OrtApiBase {
  const OrtApi*(* GetApi)(uint32_t version);   // offset 0
  const char*  (* GetVersionString)(void);     // offset 8
};
```

> **Correção ao plano anterior:** `plano_golang_lib.md:146`
> (`VerifyOffsets`) faz `a.fnAt(offGetVersionString)` a partir do
> `apiPtr` (`*OrtApi`). Isso leria um ponteiro arbitrário de dentro da
> struct errada e o chamaria. A função cujo trabalho era *detectar*
> desalinhamento de offsets era, ela própria, o desalinhamento.
>
> Efeito colateral positivo: `GetVersionString` é alcançável **sem
> nenhum offset gerado**, o que a torna o primeiro sanity check ideal.

### 2.4 Cadeia zero-CGO funciona fim a fim **[VERIFICADO]**

Spike descartável, `CGO_ENABLED=0 go build`, `purego v0.10.2`:

```
OK [1] dlopen
OK [2] OrtGetApiBase
OK [3] GetVersionString = "1.24.3"
OK [4] GetApi(24) -> *OrtApi
OK [5] CreateEnv -> 0x301e8c40           <- offset gerado, tabela alinhada
OK [6] caminho de erro: ort: Load model from /nao/existe/modelo.onnx failed...
OK [7] Release* sem crash
```

O passo [6] é o mais informativo: valida `checkStatus` (copiar mensagem →
`ReleaseStatus`) com a mensagem do ORT chegando íntegra num `error` do Go.

### 2.5 Tokenizer WordPiece em Go puro reproduz o HuggingFace **[VERIFICADO]**

Implementação Go pura (`BertNormalizer` + `BertPreTokenizer` + `WordPiece`
+ `TemplateProcessing` + `added_tokens`), comparada token a token contra
`tokenizers` 0.22.2 (a lib Rust oficial), usando o `tokenizer.json` de
`sentence-transformers/all-MiniLM-L6-v2`:

```
=== 23/23 casos idênticos ao tokenizers Rust ===
```

Corpus adversarial cobrindo: acentuação portuguesa (`ação`, `açúcar`,
`São Paulo`), diacríticos europeus (`naïve`, `Zürich`), CJK (中文, 日本語),
emoji e bandeiras, contrações, hifenização, pontuação ASCII e Unicode
(travessão `—`), espaços/tabs, palavras fora do vocabulário, números
formatados e tokens especiais literais no texto (`[CLS]`, `[SEP]`).

Dependências: **stdlib + `golang.org/x/text`** (NFD). Zero CGO.

> Este era o risco não mapeado no plano original e o mais capaz de matar
> o projeto — as opções de tokenizer existentes em Go são wrappers CGO da
> lib Rust. A verificação mostra que a família BERT/WordPiece é
> reimplementável em Go puro com fidelidade exata. **Ver limites em §5.2.**

### 2.6 Pesquisa de mercado **[VERIFICADO em fonte primária, 2026-08-26]**

Levantamento independente, lendo build tags, `switch` e constantes no
código-fonte dos projetos. **Contradiz três afirmações das versões v0.1 e
v0.2 deste documento.**

#### 2.6.1 `hugot` já é zero-CGO no build padrão — **contradiz §1.1 (v0.1)**

```
hugot_ort.go:               //go:build cgo && (ORT || ALL)
hugot_xla.go:               //go:build cgo && (XLA || ALL)
backends/tokenizer_rust.go: //go:build cgo && (ORT || XLA || ALL)
backends/tokenizer_go.go:   (sem build tag — sempre compilado)
```

`go build` sem tags produz backend Go puro. `hugot` v0.7.7 (2026-08-04),
Apache-2.0, cadência mensal, 10 pipelines. **Limite do modo puro:** motor
SimpleGo do GoMLX, **5–20× mais lento**, sem *text generation*, sem
treino, sem CUDA, cobertura incompleta de operadores, "only built/tested
on amd64-linux". É esse limite que preserva o espaço deste projeto (§1.1).

#### 2.6.2 `CGO_ENABLED=0` ≠ binário estático — **contradiz §1.1 (v0.1)**

`purego/dlfcn_nocgo_linux.go` usa
`//go:cgo_import_dynamic ... "libdl.so.2"`. O binário resultante tem
`DT_NEEDED` para `libdl.so.2` e exige loader dinâmico. Ligação 100%
estática é **incompatível** com o modelo `dlopen`-em-runtime.
**Alpine/musl: [NÃO VERIFICADO]** — o *soname* glibc está fixo no código;
precisa ser testado antes de qualquer promessa.

#### 2.6.3 Tokenizer HF em Go puro já existe, e mais completo que a §2.5

`gomlx/go-huggingface/tokenizers/hftokenizer` — **zero** ocorrências de
`import "C"`; é o que o `hugot` usa no backend Go.

| Camada | Cobertura |
|---|---|
| Modelos | WordPiece, **BPE**, **Unigram** (sem WordLevel) |
| Normalizers | NFC/NFD/NFKC/NFKD, Lowercase, StripAccents, Bert, Replace, Prepend, Sequence |
| Pre-tokenizers | Bert, Whitespace, ByteLevel, Metaspace, Split, Punctuation, Sequence |
| Post-processors | TemplateProcessing, Bert, Roberta |
| Special/added tokens | sim, com offsets |

Custo: exige **go 1.26.5**, módulo pai auto-declarado experimental.
Alternativa: `sugarme/tokenizer` (Go puro, cobre também WordLevel e
SentencePiece, go 1.23, manutenção só de *bugfix*). → Abre a §6.6.

#### 2.6.4 Armadilhas do purego v0.10.2 — **afeta §5.1 e §6.1**

- **`SyscallN` é incorreto para assinaturas com float.** Documentação
  literal: *"SyscallN does not properly call functions that have both
  integer and float parameters"*. Copia o mesmo array de `uintptr` nos
  slots inteiros **e** de float.
- **`maxArgs` = 15** na release v0.10.2 (o valor 32 está apenas no `main`
  não lançado). Precisa ser conferido contra as assinaturas do ORT.
- **`RegisterFunc(&fn, ponteiroCru)`** aceita um ponteiro de função lido
  de um offset e devolve função Go **tipada**, com float correto. É a
  saída para o problema acima — e reforça a §6.1.
- **Custo:** `RegisterFunc` usa *reflect* por chamada (~1845 ns, 44 allocs
  com 15 args, issue #399). Irrelevante para `Run` por lote; relevante em
  laço apertado.
- purego v0.10.2 (2026-07-20) ainda se auto-declara **beta**.

#### 2.6.5 Prior art de ONNX Runtime via purego

| Repo | Stars | Push | Estado |
|---|---|---|---|
| `shota3506/onnxruntime-purego` | 30 | 2026-03-15 | o mais completo; **travado em API 23**; "currently unstable" |
| `GetcharZp/onnxruntime_purego` | 22 | 2026-02-18 | — |
| `cnxxy-cn/onnxruntime_purego` | 0 | 2026-06-23 | — |

A binding madura (`yalue/onnxruntime_go`, 704 stars) é CGO, sem caminho
purego. **Conclusão: o nicho existe e não está ocupado por nada estável.**

#### 2.6.6 Versionamento do ORT — confirma §2.2

**`ORT_API_VERSION == minor version`, exatamente**, verificado tag a tag
de v1.20 a v1.29. 1.24.x → **24**, como medido na §2.2.
Última estável: **v1.29.0** (2026-08-12); `hugot` pina 1.28.0. A `.so`
desta máquina (1.24.3) está **cinco versões atrás** → ver §6.7.

---

## 3. Decisões

### 3.1 Distribuição da `libonnxruntime` — usuário fornece **[DECIDIDO]**

Ordem de descoberta, do explícito ao implícito:

```
1. ortcore.WithLibraryPath("/caminho/explicito/libonnxruntime.so")
2. $ONNXRUNTIME_LIB_PATH
3. locais padrão do SO
4. erro com instrução literal de como resolver — nunca "dlopen failed"
```

**Justificativa:** `go:embed` de binário é hostil ao module proxy do Go
(cada versão fica cacheada para sempre; 22 MB × plataformas × versões é
custo imposto a todos que rodam `go mod download`). Download em runtime
dentro de um pacote importado quebra ambientes air-gapped e CI hermético.
É o que `yalue/onnxruntime_go` e `hugot` fazem.

Módulo companheiro opcional com o binário embutido: **pós-v1**.

### 3.2 Fonte dos modelos — disco no núcleo, rede em subpacote **[DECIDIDO]**

O núcleo recebe um diretório no layout que o HuggingFace já produz
(`model.onnx`, `tokenizer.json`, `config.json`) e **nunca toca a rede**.
Um subpacote separado `hub` faz download para cache local; quem não
importa não paga. Suíte do núcleo roda integralmente offline.

### 3.3 Carregamento dinâmico é superfície de segurança **[DECIDIDO]**

`dlopen` de um caminho controlável por terceiros é execução de código
arbitrário. O carregador deve, antes de abrir:

- resolver o caminho para a forma **canônica** (`filepath.EvalSymlinks`),
- rejeitar caminhos relativos e componentes `..`,
- verificar que o arquivo **não é gravável por outros** (bit `o+w`),
- opcionalmente conferir um **checksum** fixado da versão suportada.

Erros de todos esses casos são distintos e explícitos. Isto é requisito
de v1, não polimento.

### 3.4a Política de versões — sempre a mais recente estável **[DECIDIDO — 2026-08-26]**

**Regra do Pablo:** Go e todas as dependências acompanham sempre a versão
estável mais recente disponível — **nunca** beta, RC ou tag `-pre`. Vale
tanto para o `go.mod` quanto para módulos de terceiros.

**Verificado em 2026-08-26** contra `https://go.dev/dl/?mode=json`
(única fonte autoritativa de releases estáveis do Go):

```
$ curl -s 'https://go.dev/dl/?mode=json' | jq -r '.[] | select(.stable) | .version'
go1.27.0   <- mais recente estável
go1.26.7
```

`go.mod` deve declarar `go 1.27.0`, com `toolchain go1.27.0` — isso faz o
próprio comando `go` baixar e usar o compilador certo automaticamente,
mesmo que o binário local esteja atrasado (suporte nativo desde Go 1.21).

> **Pendência não bloqueante:** o `go` instalado nesta máquina é
> **1.26.2** — mais antigo até que o 1.26.7, e duas versões menores atrás
> do 1.27.0. O `toolchain` directive cobre isso automaticamente na
> primeira compilação, mas vale atualizar o binário local também
> (`go install golang.org/dl/go1.27.0@latest` ou reinstalar via
> gerenciador de pacotes) antes da Janela 1, para não depender do
> download automático em toda máquina nova.

**Dependências de terceiros seguem a mesma regra** — sempre a última tag
estável publicada, nunca `main`/`master` não lançado. Já aplicado nas
decisões anteriores: `purego@v0.10.2` foi escolhido em vez do `main` não
lançado precisamente por essa razão (`§2.6.4` — o `main` tem `maxArgs=32`
mas não é uma versão tagueada). Antes de fixar uma dependência nova,
confirmar a tag mais recente no repositório de origem, não assumir.

### 3.4 Plataforma do v1 — linux/amd64 **[DECIDIDO]**

Única plataforma onde a suíte roda de verdade. Outras plataformas só
entram acompanhadas de CI que as execute — **nunca** declaradas como
suportadas apenas por compilarem.

### 3.5 Invariante de propriedade de memória **[DECIDIDO]**

> **Memória de propriedade do ORT nunca cruza uma fronteira de função
> dentro do `ortcore`. É copiada e liberada no mesmo lugar onde nasce.**

Uma regra, três instâncias — nenhuma abstração nova:

| Origem | Liberada por | Confinada em |
|---|---|---|
| `OrtStatus` (mensagem de erro) | `ReleaseStatus` | `checkStatus` |
| Strings do allocator (nomes de I/O) | `AllocatorFree` | `takeAllocatedString` |
| Handles (`OrtEnv`, `OrtSession`, `OrtValue`) | `Release*` | método `Close` do wrapper Go |

```go
type api struct {
	lib    uintptr
	apiPtr uintptr
	alloc  uintptr // singleton do ORT — NÃO liberar (header v1.24.3:2054)
}

// takeAllocatedString: irmã de checkStatus. Copia, libera, devolve Go.
func (a *api) takeAllocatedString(p uintptr) string {
	if p == 0 {
		return ""
	}
	s := goStringFromC(p)
	purego.SyscallN(a.fnAt(offAllocatorFree), a.alloc, p)
	return s
}
```

**Por que isso não acopla.** O allocator default é, pelo header,
*"the same pointer to the same default allocator"* e *"should NOT be
freed"* — é uma **constante de processo**, não um recurso com ciclo de
vida. Guardá-lo como campo do `api` não propaga ciclo de vida a ninguém.
Ele é conhecido por exatamente **dois** lugares: o campo e o helper.

**Nomes de I/O são lidos uma vez e cacheados.** `SessionGetInputName` /
`SessionGetOutputName` são chamados apenas no construtor da sessão; o
resultado vira `[]string` Go. Depois que `NewSession` retorna, nenhuma
string alocada pelo ORT permanece viva. O tempo de vida fica limitado por
um construtor, não pela sessão.

**Verificação mecânica.** Um teste faz *grep* no próprio pacote e falha se
`offReleaseStatus`, `offGetErrorMessage` ou `offAllocatorFree` aparecerem
fora dos arquivos autorizados. O invariante é executável, não documental.

### 3.6 `runtime.Pinner` é obrigatório na passagem de memória Go **[DECIDIDO]**

Toda memória alocada em Go e **retida** pela API nativa é fixada com
`runtime.Pinner`, com `Unpin` pareado. `runtime.KeepAlive` **não** basta:
ele garante liveness, não imobilidade.

**Precisão técnica (relevante para a dissertação):** o GC do Go hoje não
move objetos do heap, portanto o código funciona sem `Pinner` — *por
acidente de implementação, não por contrato*. E como não há CGO, o
`cgocheck` do runtime não existe para emitir aviso. É a classe de defeito
que passa em toda a suíte e quebra numa versão futura do runtime.

**Consequência de design:** `CreateTensorWithDataAsOrtValue` faz o ORT
reter o ponteiro por toda a vida do `OrtValue` — não pela duração da
chamada. Logo o `Pinner` **não pode** ser local com `defer Unpin()`:

```go
type Tensor struct {
	value  uintptr        // OrtValue
	data   []float32      // buffer Go, propriedade nossa
	pinner runtime.Pinner // vive tanto quanto value
}

func (t *Tensor) Close() {
	purego.SyscallN(fnAt(offReleaseValue), t.value)
	t.pinner.Unpin() // pareado, e SÓ depois do Release
}
```

A ordem importa: `Unpin` antes de `ReleaseValue` reabriria a janela que o
`Pin` existe para fechar.

---

## 4. Arquitetura

Quatro subsistemas independentes. O `plano_golang_lib.md` original
detalha o primeiro e assume os outros três.

```
┌─────────────────────────────────────────────────────────┐
│  4. embeddings  — API pública idiomática                │
│     Embed(ctx, []string) ([][]float32, error)           │
└────────────┬────────────────────────┬───────────────────┘
             │                        │
┌────────────▼──────────┐  ┌──────────▼────────────────────┐
│ 2. tokenizer          │  │ 3. pooling                    │
│    tokenizer.json →   │  │    mean pooling + L2 norm     │
│    ids + attn mask    │  │    (guiado por attention mask)│
│    Go puro + x/text   │  │    stdlib pura                │
└───────────────────────┘  └──────────┬────────────────────┘
                                      │
┌─────────────────────────────────────▼───────────────────┐
│ 1. ortcore — fronteira FFI zero-CGO                     │
│    dlopen · tabela de offsets gerada · checkStatus      │
│    tensores · ciclo de vida (Env/Session/Value)         │
└─────────────────────────────────────────────────────────┘
```

| # | Pacote | Responsabilidade | Depende de | Risco |
|---|---|---|---|---|
| 1 | `ortcore` | Fronteira FFI. Único lugar com `unsafe` e `uintptr`. | purego | Alto — **mitigado, §5.1** |
| 2 | `tokenizer` | `tokenizer.json` → ids + máscara | `x/text` | Médio — **§5.2** |
| 3 | `pooling` | mean pooling, L2 normalize | — | Baixo |
| 4 | `embeddings` | API pública, orquestração | 1,2,3 | Baixo |

**Regra de fronteira:** `uintptr`, `unsafe.Pointer` e qualquer ponteiro cru
**nunca** cruzam para fora do `ortcore`. A API pública expõe apenas
`[]float32`, `[]int64`, structs e `error`.

---

## 5. Riscos e mitigações

### 5.1 Cegueira de tipos na fronteira purego

**A crítica original está correta no diagnóstico, mas datada na
prescrição** — ela foi escrita contra a premissa de que as funções do ORT
seriam registradas via `purego.RegisterFunc`. Pela §2.1, isso vale para
exatamente **um** símbolo (`OrtGetApiBase`). Todo o resto passa por
`purego.SyscallN`.

Isso torna o problema **pior**, não melhor:

```go
// purego.SyscallN(fn uintptr, args ...uintptr)
purego.SyscallN(fnAt(offCreateSession), env, path, opts, out)  // 4 args — certo
purego.SyscallN(fnAt(offCreateSession), env, path, out)        // 3 args — COMPILA
```

Com `RegisterFunc` ainda haveria um tipo Go declarado, revisável lado a
lado com o header. Com `SyscallN` **não há assinatura nenhuma**: aridade,
ordem e semântica dos argumentos são invisíveis ao compilador. Um ponteiro
simples onde a API espera duplo (`OrtSession**`) não gera nem aviso.

**Mitigação — automação, não vigilância.** Dado que as janelas de trabalho
são curtas e espaçadas, revisão manual recorrente contra o header é uma
estratégia que falha por atrito. A geração já resolve os offsets; ela deve
resolver também as assinaturas:

> O gerador (`internal/ortgen`) emite, além da tabela de offsets, um
> **wrapper Go tipado por função**, com aridade e tipos derivados do
> header. Os `SyscallN` crus ficam confinados ao arquivo gerado; o código
> escrito à mão chama apenas os wrappers e volta a ter checagem do
> compilador Go.

Isso transforma "revisar 250 assinaturas a cada versão do ORT" em "rodar
`go generate` e compilar". **[RESOLVIDO — ver §6.1, J1 fechada em 2026-08-27]**

### 5.2 Ownership de memória do ORT

**A crítica original está integralmente correta e foi validada** no passo
[6] da §2.4. `GetErrorMessage` devolve ponteiro para memória do ORT, que o
GC do Go não rastreia. O padrão implementado e testado:

```go
func checkStatus(st uintptr) error {
    if st == 0 { return nil }
    msgPtr, _, _ := purego.SyscallN(fnAt(offGetErrorMessage), st)
    msg := goStr(msgPtr)                              // cópia ANTES
    purego.SyscallN(fnAt(offReleaseStatus), st)       // liberar DEPOIS
    return fmt.Errorf("ort: %s", msg)
}
```

**Invariante do pacote:** `GetErrorMessage` e `ReleaseStatus` só podem ser
chamadas de dentro de `checkStatus`. Nenhum outro ponto do código. Isso é
verificável mecanicamente por um teste que faz grep no próprio pacote.

O mesmo problema se aplica a `SessionGetInputName`/`GetOutputName`
(memória do *allocator*) e a `GetTensorMutableData` (aponta para o buffer
do `OrtValue`, válido só enquanto o valor viver). **Ambos resolvidos pelo
invariante único da §3.5**, sem introduzir abstração nem propagar o
allocator pela API.

### 5.3 Isolamento do artefato de teste

**Crítica original correta, com uma ressalva de escopo.** O modelo
sintético de adição valida a fronteira FFI, mas **não** valida o pipeline
de embeddings — não tem tokenizer, nem pooling, nem valores de referência.
Portanto, dois níveis:

| Nível | Artefato | Onde | Valida |
|---|---|---|---|
| A | Modelo sintético de adição (~KB) | commitado em `testdata/` | `ortcore` isolado |
| B | MiniLM-L6-v2 real + vetores de referência do Python | baixado no CI, **não** commitado | pipeline fim a fim |

O script Python que gera (A) fica no repositório como documentação e é
excluído do CI. A suíte Go **nunca** invoca Python. Os vetores de
referência de (B) são gerados uma vez e commitados como JSON.

### 5.4 Deriva de versão do ONNX Runtime

Offsets são válidos para **uma** `ORT_API_VERSION`. Um `.so` de outra
versão produz chamada silenciosa à função errada.

**Mitigação:** no `Load`, antes de qualquer outra coisa, comparar
`GetVersionString()` (§2.3 — alcançável sem offsets) com a versão pinada e
**falhar explicitamente** se divergir. Barato, e é o único ponto onde o
erro ainda é detectável.

---

## 6. Questões em aberto

### 6.1 Escopo do gerador **[DECIDIDO — 2026-08-26]**

**Decisão do Pablo:** desenho aceito na íntegra. Verificação de assinatura
via atribuição no `dump_offsets.c` (compilador C valida), gerador emite
wrappers via `purego.RegisterFunc` — nunca `SyscallN` cru — encerrando
§5.1 e a falha de float da §2.6.4 de forma estrutural, não por disciplina
de revisão.

A §5.1 pede wrappers tipados, mas a objeção óbvia é que gerá-los exigiria
**parsear o header C** — complexidade desproporcional para o projeto.

**Proposta que evita o parser:** as assinaturas são declaradas uma única
vez no próprio `dump_offsets.c`, e a **verificação fica a cargo do
compilador C**, por atribuição:

```c
// Se a assinatura declarada divergir do header, isto é ERRO DE COMPILAÇÃO
// (com -Werror=incompatible-pointer-types), não corrupção silenciosa.
static OrtStatus* (*chk_CreateEnv)(OrtLoggingLevel, const char*, OrtEnv**);
...
chk_CreateEnv = ((OrtApi*)0)->CreateEnv;
```

O gerador então emite, da mesma declaração, o offset **e** o wrapper Go
tipado. Nenhuma linha de C é parseada; o compilador C faz o *type checking*
que o Go não pode fazer. Uma versão nova do ORT que mude uma assinatura
falha em `go generate`, e não em produção.

**Reforço vindo da §2.6.4:** os wrappers gerados devem usar
`purego.RegisterFunc(&fn, a.fnAt(offX))` — que aceita um ponteiro de
função cru e devolve função Go tipada — em vez de `purego.SyscallN`.
Isso resolve **dois** problemas de uma vez:

| Problema | `SyscallN` | `RegisterFunc` |
|---|---|---|
| Aridade/tipos conferidos pelo Go | não | **sim** |
| Assinaturas com float | **incorreto** (§2.6.4) | correto |
| Custo por chamada | mínimo | ~1845 ns / 44 allocs |

O custo de *reflect* é irrelevante para `Run` (uma chamada por lote) e
para o construtor da sessão. Se algum ponto se revelar quente, aquele
caso específico pode voltar a `SyscallN` **desde que sua assinatura não
tenha float** — decisão local, medida, não global.

**Custo honesto:** o gerador cresce, e cada função suportada exige uma
declaração no `.c`. Para as ~25 funções do v1, é aceitável.

> **Fechamento (J1, 2026-08-27):** implementado em `internal/ortgen`
> exatamente como descrito acima, com um reforço encontrado só na revisão
> final de branch inteiro da J1: a atribuição em `dump_offsets.c`
> verifica a assinatura **C** contra o header, mas a string do tipo
> **Go** (`GOSIG`, na mesma linha de `ORT_FUNCTIONS`) era digitada à mão
> e nada a conferia — um `GOSIG` com um parâmetro a menos compilava e
> gerava sem erro. Fechado emitindo, para cada função, um comentário `//
> C: <assinatura>` logo acima do tipo Go gerado (`PRINT_TYPE` em
> `dump_offsets.c`) e cross-checando a **aridade** dos dois lados dentro
> de `generate()` (`internal/ortgen/main.go`); uma divergência agora
> falha `go generate`, com o nome da função. Prova em
> `internal/ortgen/main_test.go`:
> `TestGenerate_DivergentGoSignatureFailsArityCheck`.
>
> **Nota sobre o padrão de emissão de offsets:** o gerador emite cada
> offset como uma constante individual (`const offNAME = valor`, uma por
> linha), não como um bloco `const (...)` agrupado — mesmo que um bloco
> agrupado fosse a forma mais óbvia de esboçar isso. Motivo: `go/format`
> realinha um bloco `const (...)` ao identificador mais longo do grupo
> (preenchendo com espaços até o `=`), o que quebra buscas por substring
> exato como `offCreateEnv = 24` nos testes do gerador — o texto gerado
> passaria a ter um número variável de espaços dependendo de quais outras
> constantes estão no mesmo bloco. Registrado aqui para quem escrever o
> próximo gerador não tropeçar no mesmo lugar.

### 6.2 Modelo de memória dos tensores **[RESOLVIDO → §3.6]**
`runtime.Pinner` obrigatório, pareado com `Unpin`, com o `Pinner` vivendo
dentro do struct do tensor. Resolvido pela sua crítica de 2026-08-26.

### 6.3 Nome, módulo e licença **[DECIDIDO — 2026-08-26]**

- **Licença: Apache-2.0.** Concessão explícita de patentes — mandatória
  num nicho patrocinado por Microsoft/Meta/Google. Compatível com o MIT
  do ONNX Runtime.
- **Layout do módulo:** subpacotes na raiz — `<módulo>/ortcore`,
  `<módulo>/tokenizer`, `<módulo>/pooling`, `<módulo>/embeddings`.
- **Caminho completo do módulo:** `github.com/pablobelmiro/goembed`.
  Nome do projeto: **goembed**.

### 6.4 Escopo do tokenizer **[RESOLVIDO — só família BERT]**
O v1.0 declara suporte a **WordPiece / BERT** apenas, que é o verificado
na §2.5. BPE (GPT/RoBERTa) e Unigram (SentencePiece/T5) ficam fora, e a
documentação diz isso explicitamente em vez de omitir. Declarar menos e
cumprir. Resolvido pela sua crítica de 2026-08-26.

### 6.5 Documentos ausentes **[RESOLVIDO → §7]**
"Seção 16.3" e "Anexo A" existiam apenas em estado volátil. O conteúdo
tático foi reconstruído e passa a viver na §7 deste documento, que é a
fonte única. `plano_golang_lib.md` fica como registro histórico, não como
referência ativa — suas correções estão na §2.2 e §2.3.

### 6.6 Tokenizer: construir ou reusar **[DECIDIDO — 2026-08-26]**

**Decisão do Pablo: construir**, honrando estritamente o escopo BERT /
WordPiece da §6.4.

> **Justificativa (registrada em nome próprio):** para uma dissertação de
> Mestrado no **PPComp — IFES, Campus Serra**, ter uma contribuição
> primária que implementa o algoritmo e demonstra suas garantias
> computacionais (normalização NFC/NFKC, WordPiece, verificação byte-a-
> -byte contra a referência Rust) tem peso perante a banca que uma
> dependência importada — ainda mais uma auto-declarada experimental —
> não tem. Reusar barateia a argumentação acadêmica nesta etapa
> específica.

A tabela abaixo é preservada como registro do trade-off considerado:

A §2.5 provou que dá para construir. A §2.6.3 mostrou que **já existe
pronto e mais completo**. São coisas diferentes, e a decisão mudou de
natureza: não era mais "é viável?", era "vale a pena?".

| Opção | A favor | Contra |
|---|---|---|
| **Reusar `hftokenizer`** | pronto, cobre BPE e Unigram, validado em produção pelo `hugot`, corta a Janela 5 inteira | dependência de módulo **auto-declarado experimental**; exige go 1.26.5; perde-se controle sobre um componente central |
| **Reusar `sugarme/tokenizer`** | Go puro, cobre até SentencePiece, go 1.23 | manutenção só de *bugfix* desde 2025-09 |
| **Construir (§2.5)** | controle total, 23/23 verificado, superfície mínima, sem dependência experimental, **é contribuição própria da dissertação** | custa uma janela; cobre só BERT (§6.4); risco de divergência em *edge cases* não testados |

> **Peso da dissertação:** este é o único subsistema onde "já existe" pode
> ser um argumento *fraco* — um componente reimplementado e verificado
> contra a referência Rust é resultado defensável; uma dependência
> importada não é. Mas isso é julgamento acadêmico, e é seu.
>
> **Recomendação:** construir, mantendo o escopo BERT da §6.4. A §2.5 já
> mostrou que o custo real é baixo, e a independência de um módulo
> experimental vale mais do que a janela economizada.

### 6.7 Versão do ORT a pinar **[DECIDIDO e VERIFICADO — 2026-08-26]**

**Decisão do Pablo: pinar em 1.28.0 (ORT_API_VERSION 28)**, alinhando com
o que o `hugot` usa em produção (§2.6.1), em vez de nascer atrelado a uma
versão que já estava cinco releases atrás.

**Verificação feita antes de aceitar a decisão como fechada** — porque
trocar a versão pinada invalida a premissa sob a qual toda a §2 foi
verificada (contra 1.24.3). Baixei `onnxruntime_c_api.h` da tag `v1.28.0`
e comparei a saída do `dump_offsets.c` (§2.2) contra as 25 funções do
steel thread:

```
=== 1.28.0 ===
ORT_API_VERSION = 28
sizeof(OrtApi)  = 3392 bytes (424 ponteiros)   // era 415 na 1.24.3

diff (1.24.3 esquerda / 1.28.0 direita):
< ORT_API_VERSION = 24 / sizeof = 3320 (415 ponteiros)
> ORT_API_VERSION = 28 / sizeof = 3392 (424 ponteiros)
(nenhuma outra linha diverge — todos os 25 offsets são IDÊNTICOS)
```

**Achado que fica registrado como fato de arquitetura, não coincidência:**
a struct cresceu 9 ponteiros entre 1.24 e 1.28, mas as 25 funções do v1.0
mantiveram o offset exato — porque os campos novos foram **anexados ao
final** da `OrtApi` nesse intervalo. Isso **não é uma garantia da API C**,
é um comportamento observado numa janela de versões; uma futura inserção
de campo no meio da struct quebraria isso sem aviso de compilação. É
exatamente por isso que o `Load()` **sempre** confere `GetVersionString()`
(§2.3) contra a versão pinada como primeiro passo — essa checagem
continua obrigatória mesmo com offsets estáveis observados aqui.

**Ação necessária, sem custo de janela:** baixar `libonnxruntime.so.1.28.x`
(hoje só há a `.so.1.24.3` do venv Python nesta máquina) e o header/tag
`v1.28.0` completo antes de abrir a Janela 1 — é precisamente o "custo
imediato" que você já havia antecipado.

#### 6.7.1 Pendência resolvida — artefato adquirido e verificado ao vivo **[RESOLVIDO — 2026-08-26]**

Baixado o release oficial `onnxruntime-linux-x64-1.28.0.tgz` (assets da
tag `v1.28.0`, GitHub), não a `.so` de um pacote Python. Fica em
**`~/.cache/goembed/onnxruntime/1.28.0/`** (fora do repositório, nunca
commitado — coerente com a §3.1: é o mesmo caminho que um usuário real
apontaria via `$ONNXRUNTIME_LIB_PATH`).

```
$ nm -D --defined-only libonnxruntime.so.1.28.0 | grep -c " T "
2                                    # confirma §2.1 também na 1.28.0
```

Reexecutei o spike zero-CGO da §2.4 (mesmo binário Go, `CGO_ENABLED=0`)
contra o `.so` real 1.28.0, não apenas contra o header estático:

```
OK [3] GetVersionString = "1.28.0"
OK [4] GetApi(28) -> *OrtApi
OK [5] CreateEnv -> 0xa429850          // offsets da 1.24.3 continuam válidos
OK [6] caminho de erro: ort: Load model from ... failed. File doesn't exist
>>> PREMISSA ZERO-CGO: CONFIRMADA (1.28.0)
```

**Achado adicional — o contrato exato do `GetApi`, mais preciso que a
formulação anterior.** Testei as três chamadas:

| Chamada | Runtime | Resultado |
|---|---|---|
| `GetApi(24)` | 1.28.0 | **sucesso** — devolve a struct atual, não uma versão histórica menor |
| `GetApi(28)` | 1.28.0 | sucesso |
| `GetApi(29)` | 1.28.0 | `nil`, com mensagem do próprio ORT: *"only API versions [1, 28] are supported... Current ORT Version is: 1.28.0"* |

`GetApi(24)` ter sucedido contra o runtime 1.28.0 prova, na prática, o que
a §6.7 já inferia da ABI aditiva: **o requisito é `versão_pedida ≤
versão_do_runtime`, não igualdade.** O runtime sempre devolve a struct
atual e completa — nunca uma struct histórica truncada.

**Consequência de segurança, mais precisa que a redação anterior:** o
`GetApi` protege contra um binário **atrasado demais** para os offsets
gerados (pede versão alta, `.so` velho devolve `nil`). Ele **não**
protege — nem tem como proteger — contra a suposição implícita de que a
ABI seguirá aditiva para sempre. Essa suposição é a única coisa que
sustenta os offsets estáveis medidos acima. É por isso que a checagem de
`GetVersionString()` (§2.3) permanece obrigatória como primeiro passo do
`Load()`: ela é a única verificação deste desenho que não depende dessa
suposição continuar valendo nas versões futuras.

### 6.8 Pendências técnicas de baixo risco, herdadas da J1 **[itens 1-2 RESOLVIDOS — 2026-08-28]**

Levantadas pela re-revisão final da J1 (a última rodada de correção
permitida pelo processo daquela janela). Nenhuma bloqueava a J2.

1. **[RESOLVIDO — 2026-08-28] O guarda de aridade Go era ele mesmo
   desguarnecido.** O mecanismo que fecha o achado da §6.1 (assinatura
   C verificada pelo compilador + aridade do tipo Go cross-checada em
   `generate()`) dependia das linhas `// C: <assinatura>` que
   `dump_offsets.c` emite acima de cada tipo Go — se elas sumissem
   numa edição futura, `checkGoSignatureArity` não encontrava nada
   para comparar e retornava sucesso silencioso. Corrigido:
   `checkGoSignatureArity` agora conta `type fn` declarações no
   fonte gerado e exige que o mesmo número tenha sido pareado com um
   comentário `// C: ` — se qualquer tipo ficar sem checagem, a
   geração falha. Prova: `TestGenerate_MissingSignatureCommentFailsArityCheck`
   remove a linha que emite o comentário e confirma que `generate()`
   passa a errar (antes da correção, isso passava em silêncio).
2. **[RESOLVIDO — 2026-08-28] 2 dos 27 tipos Go ficavam fora do
   cross-check.** `fnGetApi` e `fnGetVersionString` (os campos de
   `OrtApiBase`, emitidos fora do X-macro `ORT_FUNCTIONS`) agora
   recebem comentário `// C: ` como os demais — `const OrtApi*(*)(uint32_t)`
   e `const char*(*)(void)`, os mesmos textos já usados nos
   `_Static_assert` correspondentes. Cobertura: 27/27.
3. **O guarda checa só aridade, não tipo.** Trocar `int32` por
   `float64` num parâmetro passaria sem erro de geração — vira bug de
   runtime em `purego.RegisterFunc` (J2), não de build. Escopo
   intencional da correção da J1, documentado no código; registrado
   aqui para não ser esquecido quando a J2 começar a consumir esses
   tipos. **Sem ação — não é defeito, é limite de escopo conhecido.**

---

## 7. Plano tático da v1.0

**Restrição de formato:** as janelas de trabalho são de ~2 dias, uma vez
por semana, concorrendo com expediente integral e estudo para concursos.
Portanto **cada incremento abaixo termina em estado compilável, testado e
commitado**, e é retomável lendo apenas o commit anterior e este
documento — sem depender de histórico de conversa.

Um incremento que não cabe numa janela está mal fatiado e deve ser
dividido antes de começar, não no meio.

| # | Incremento | Entrega verificável | Encerra |
|---|---|---|---|
| **J0** | *Decisões de bloqueio* | ~~§6.1, §6.3, §6.6, §6.7~~ **fechadas em 2026-08-26** | trava J1 |
| **J1** | `internal/ortgen` | ~~`go generate ./...` reproduz `ortapi_gen.go`; compila com `CGO_ENABLED=0`~~ **fechada em 2026-08-27** | §6.1 |
| **J2** | `ortcore`: carga e erros | ~~Carrega a `.so` real, versão confere, path inválido dá erro tipado~~ **fechada em 2026-08-28** | §3.3, §5.2 |
| **J3** | `ortcore`: sessão e metadados | Abre o modelo sintético, lista nomes de I/O, `Close` sem vazamento | §3.5 |
| **J4** | `ortcore`: tensores e `Run` | **Steel thread fechado:** modelo de adição devolve a soma correta | §3.6, §6.2 |
| **J5** | `tokenizer` | Tabela de testes 23/23 contra vetores do Rust commitados | §2.5, §6.6 |
| **J6** | `pooling` + `embeddings` | Embedding do MiniLM real bate com o vetor de referência do Python | — |
| **J7** | Empacotamento | README, godoc, CI, exemplo, tag `v0.1.0` | §1, §6.3 |

### 7.1 Marcos de decisão

- **Fim de J4 — go / no-go.** Se o steel thread não fechar aqui, a
  premissa "ORT nativo sem CGO" (§1.1) está morta na prática, mesmo tendo
  passado no spike. A resposta madura então é **usar o `hugot`** — que já
  entrega tanto o caminho zero-CGO lento quanto o caminho CGO rápido
  (§2.6.1) — e preservar o trabalho de tokenizer, que é independente.
  Escrito agora, de cabeça fria, para não ser decidido cansado na sétima
  semana.
- **Fim de J6 — congelamento de escopo.** Nada entra no v1.0 depois disso;
  ideias novas viram *issues* do v1.1.

### 7.2 O que está explicitamente fora do v1.0

Registrado para impedir deriva de escopo em janelas futuras:
GPU / execution providers · BPE e Unigram (§6.4) · pipelines além de
embeddings · download automático de modelos (subpacote `hub`) · módulo
companheiro com a `.so` embutida · plataformas além de linux/amd64 (§3.4)
· treino, *fine-tuning* e quantização.

---

## 8. Histórico

| Data | Alteração |
|---|---|
| 2026-08-26 | v0.1 — escopo definido; spikes de FFI e tokenizer executados e confirmados; correções a `ORT_API_VERSION` (20→24) e a `VerifyOffsets`; crítica de FFI incorporada em §5. |
| 2026-08-26 | v0.2 — crítica do Pablo incorporada: `runtime.Pinner` obrigatório (§3.6); invariante único de propriedade de memória resolve o acoplamento do `AllocatorFree` (§3.5); tokenizer do v1 limitado à família BERT (§6.4); plano tático reconstruído e internalizado (§7). Proposta de gerador com verificação de assinatura pelo compilador C (§6.1) aguarda crítica. |
| 2026-08-26 | v0.3 — **pesquisa de mercado (§2.6) refuta duas premissas da §1.1**: `hugot` já é zero-CGO no build padrão, e `CGO_ENABLED=0` com purego não gera binário estático. Proposta de valor **reformulada** para "velocidade nativa do ORT sem CGO no build". Descobertas adicionais: `SyscallN` é incorreto para assinaturas com float e `maxArgs`=15 na v0.10.2 (§2.6.4) → §6.1 passa a exigir `RegisterFunc`; existe tokenizer HF em Go puro mais completo que a §2.5 → abre §6.6; `ORT_API_VERSION == minor` confirmado → abre §6.7. |
| 2026-08-26 | v0.4 — **J0 fechada.** §6.1 (gerador via `RegisterFunc`), §6.3 (Apache-2.0, módulo `github.com/<usuário>/goembed`), §6.6 (tokenizer construído — justificativa acadêmica: PPComp/IFES Serra) e §6.7 (pinar ORT 1.28.0) decididos pelo Pablo. §6.7 verificada antes de aceitar: offsets das 25 funções do steel thread são **idênticos** entre 1.24.3 (415 ponteiros) e 1.28.0 (424 ponteiros) — campos novos anexados ao final, não é garantia da API C. Módulo fechado: `github.com/pablobelmiro/goembed`. Pendência restante para abrir J1: `.so`/header 1.28.0 nesta máquina. |
| 2026-08-26 | v0.5 — **Pendência do §6.7 resolvida** (§6.7.1): release oficial ORT 1.28.0 baixado para `~/.cache/goembed/onnxruntime/1.28.0/` (fora do repo); spike zero-CGO reexecutado contra o binário real, confirmado; contrato exato do `GetApi` verificado (`versão ≤ runtime`, struct sempre completa e atual). Repositório git inicializado, primeiro commit feito. Harness dos agentes: `CLAUDE.md` do projeto criado. Nova política §3.4a: Go e dependências sempre na última versão estável publicada (nunca beta/RC/`main`) — Go local (1.26.2) está atrasado frente à estável atual (1.27.0). `LOG_DEVELOPMENT.md` criado como diário de sessões. |
| 2026-08-27 | v0.6 — **J1 fechada**: `go.mod` bootstrapped (`github.com/pablobelmiro/goembed`, `go 1.27.0`; um `toolchain go1.27.0` redundante foi commitado e depois removido via `go mod tidy`, descoberto porque `go generate`/`go build` recusavam rodar até ele sair). Gerador `internal/ortgen` implementado (SDD com 4 tarefas) e revisado numa revisão final de branch inteira (revisor sênior, reprodução independente empírica) — veredito "Ready to merge — With fixes". Achados Important da revisão corrigidos na mesma janela: (1) a verificação de assinatura cobria só o lado C (`_Static_assert` contra o header); o tipo Go (`GOSIG`) era digitado à mão sem checagem nenhuma — fechado emitindo a assinatura C como comentário acima de cada tipo Go gerado e cross-checando aridade em `generate()` (ver nota na §6.1); (2) erros de `cc`/do binário gerado descartavam o `err` original, mascarando "compilador ausente" como erro genérico; (3) esta própria atualização da §6.1, §7 e deste histórico, que tinha ficado pendente ao fechar a J1. Também corrigidos, na mesma leva: `TestSteelThreadOffsets` reestruturado de dois `map` paralelos para uma slice de structs cobrindo todos os offsets emitidos (não só o subconjunto original); `gofmt` aplicado a `ortcore/ortapi_gen_test.go`; comentário de topo de `dump_offsets.c` traduzido e limpo de referências a números de task efêmeros; `ortcore/generate.go` documenta a exigência de `ORT_HEADER_DIR`; o gerador respeita `$CC` (com fallback `cc`); o cabeçalho gerado de `ortapi_gen.go` agora nomeia a versão do ORT (`v1.28.0`). Nota de arquitetura registrada na §6.1: o gerador emite offsets como constantes individuais, não como bloco `const (...)` agrupado, porque `go/format` realinharia o bloco e quebraria buscas por substring exato nos testes. |
| 2026-08-27 | v0.7 — **Revisão final da J1 fechada e enviada ao remote.** Re-revisão independente da leva de correção acima (modelo mais capaz, reprodução própria com mutações diferentes das do implementador, casos de borda do checador de aridade) confirmou os 3 Important + 8 Minor como corrigidos, sem quebra nova. 5 residuais levantados pela própria re-revisão, adjudicados nesta sessão: 3 registrados como pendência de baixo risco (§6.8 — o guarda de aridade Go não se autoprotege se o comentário `// C:` sumir; 2 dos 27 tipos ficam fora do cross-check; o guarda checa só aridade, não tipo) e 2 sem ação necessária (string de versão do ORT no cabeçalho gerado já mitigada por comentário; referência quebrada no `LOG_DEVELOPMENT.md` corrigida na hora). `git push -u origin master` feito com sucesso para `git@github.com:pablobelmiro/goembed.git` (repo remoto estava vazio). Workspace do plano (`.superpowers/sdd/`) removido — histórico do git é o registro permanente. **Sessão pausada aqui, a pedido do Pablo**, para retomar depois com planejamento da J2. |
| 2026-08-28 | v0.8 — **Itens 1 e 2 da §6.8 fechados.** `checkGoSignatureArity` agora conta `type fn` vs `// C: ` e falha se algum tipo gerado ficar sem checagem (prova: `TestGenerate_MissingSignatureCommentFailsArityCheck`, que remove a linha do comentário e confirma o erro — antes passava em silêncio). `fnGetApi`/`fnGetVersionString` (OrtApiBase) ganharam comentário `// C: `, fechando a cobertura em 27/27. Geração reproduzida (byte-idêntica), suíte inteira passa (`CGO_ENABLED=0 build/vet/test`), `gofmt` limpo. Item 3 da §6.8 permanece sem ação — é escopo intencional, não defeito. |
| 2026-08-28 | v0.9 — **J2 fechada**: `ortcore.Load()`, `WithLibraryPath()` e `Close()` implementados e verificados contra o binário real de ORT 1.28.0. Carregamento dinâmico segue a ordem de descoberta (§3.1: explícito > env > padrão); validação de caminho (§3.3: canônico, sem `..`, não gravável por outros) com testes independentes para cada check sem tocar a `.so` real; versão verificada primeiro (§5.4) via `checkVersion` como função pura testável; `checkStatus` confinado a um único call site verificável por grep (§3.5); checksum deferred (mencionado como opcional na spec). Suíte: 23 testes (4 em `internal/ortgen`, 19 em `ortcore`) passam; `go vet -unsafeptr=false` sem achados; `gofmt` limpo. Spec coverage: §3.1 (ordem de descoberta), §3.3 (validação de caminho — cada check independente), §5.4 (version check como função pura), §3.5/§5.2 (`checkStatus` único call site). Próximo passo: J3 (`ortcore`: sessão e metadados). |
