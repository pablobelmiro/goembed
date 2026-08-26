# goembed — instruções do projeto

Lib Go de embeddings de texto via ONNX Runtime, sem CGO no build.
Dissertação de Mestrado (PPComp — IFES, Campus Serra).

## Antes de tocar em qualquer código

**A fonte única de verdade é `ARQUITETURA_OFICIAL.md`, na raiz deste
repositório.** Leia-o inteiro antes de propor ou escrever qualquer coisa.
Ele está rotulado por seção como `[VERIFICADO]` (testado, com evidência),
`[DECIDIDO]` (escolha feita, com justificativa) ou `[EM ABERTO]`. Trate
`[EM ABERTO]` como bloqueio real — pare e pergunte, não assuma.

`plano_golang_lib.md` é **histórico, não referência ativa** — duas de
suas afirmações centrais foram corrigidas em `ARQUITETURA_OFICIAL.md §2`.

`LOG_DEVELOPMENT.md` é o diário de sessões. Leia a entrada mais recente
para saber onde a última sessão parou antes de continuar.

## Decisões que já estão fechadas (não reabrir sem discutir)

- **Sempre a versão estável mais recente — Go e dependências.** Nunca
  beta, RC ou `main`/`master` não lançado. `go.mod` usa a última release
  estável do Go (`go` + `toolchain` directives) — confira em
  `https://go.dev/dl/?mode=json` (campo `stable`) antes de fixar, não
  assuma pelo que já está instalado localmente. Para dependências
  Go, use a última tag publicada do repositório, não `main`.
- **Módulo:** `github.com/pablobelmiro/goembed`. Licença Apache-2.0.
- **ONNX Runtime pinado em 1.28.0** (`ORT_API_VERSION = 28`). O artefato
  vive em `~/.cache/goembed/onnxruntime/1.28.0/` — **fora do repo, nunca
  commitado.** Aponte `ONNXRUNTIME_LIB_PATH` para lá em desenvolvimento.
- **Zero CGO em todo o pipeline** (`CGO_ENABLED=0` deve compilar e passar
  a suíte inteira, sempre). Se uma dependência nova exigir CGO, isso é um
  bloqueio de arquitetura, não um detalhe de implementação — pare e avise.
- **Tokenizer é construído, não importado** (WordPiece/BERT apenas — ver
  `ARQUITETURA_OFICIAL.md §6.4` e `§6.6`). Não adicione
  `daulet/tokenizers`, `hftokenizer` ou qualquer wrapper CGO de tokenizer.
- **Núcleo (`ortcore`, `tokenizer`, `pooling`) nunca toca rede.** Download
  de modelo é responsabilidade exclusiva de um subpacote `hub`, ainda não
  implementado — ver `§3.2`.

## Zona de alto escrutínio: `ortcore`

O pacote `ortcore` é a fronteira FFI: o único lugar do projeto com
`unsafe`, `uintptr` e offsets gerados. Regras que valem mais aqui do que
em qualquer outro lugar do repositório:

1. **Nunca digite um offset à mão.** Todo offset vem de
   `internal/ortgen` (`go generate`), nunca de edição manual do arquivo
   gerado. Se um offset parecer errado, pare — não "corrija" por conta
   própria (ver o prompt original em `plano_golang_lib.md §5`, que
   continua valendo como princípio mesmo com o plano tático revisado).
2. **Wrappers da fronteira usam `purego.RegisterFunc`, nunca
   `purego.SyscallN` cru** (`§5.1`, `§6.1`). `SyscallN` é comprovadamente
   incorreto para assinaturas com float — não é preferência de estilo.
3. **Memória de propriedade do ORT nunca cruza uma fronteira de função**
   dentro do pacote — é copiada e liberada no mesmo lugar onde nasce
   (`§3.5`). `GetErrorMessage`/`ReleaseStatus` só dentro de `checkStatus`;
   `AllocatorFree` só dentro de `takeAllocatedString`. Isso é verificável
   por grep — não introduza um segundo call site.
4. **`uintptr`/`unsafe.Pointer` nunca cruzam para fora de `ortcore`.** A
   API pública do projeto expõe apenas tipos Go seguros.
5. **Toda memória Go retida pela API nativa usa `runtime.Pinner`**,
   pareado com `Unpin` no `Close()` do wrapper — nunca `defer` local
   (`§3.6`). `runtime.KeepAlive` sozinho não é suficiente.

## Convenções de teste

- A suíte inteira roda com `CGO_ENABLED=0 go test ./...` e **sem rede**
  — exceto testes do subpacote `hub`, que são os únicos que podem tocar
  a internet (e devem ser marcados/isoláveis por build tag ou `-short`).
- Modelo sintético de teste em `testdata/` (`§5.3` nível A) valida
  `ortcore` isolado. Vetores de referência do MiniLM real (`§5.3` nível
  B, gerados uma vez em Python) validam o pipeline fim a fim e são
  commitados como JSON — a suíte Go nunca invoca Python.
- Plataforma real de teste: **linux/amd64**, único ambiente disponível
  (`§3.4`). Não declare outra plataforma como suportada só por compilar.

## Disciplina de commit

Trabalho acontece em janelas curtas e espaçadas (`§7`). Cada incremento
(J1, J2, ...) **termina compilável, testado e commitado** — nunca deixe
o repositório em estado quebrado ao final de uma sessão. Ao retomar, o
contexto necessário é: o commit anterior + `ARQUITETURA_OFICIAL.md` +
a última entrada do `LOG_DEVELOPMENT.md`. Não assuma memória de conversa.

Ao final de cada sessão significativa, adicione uma entrada em
`LOG_DEVELOPMENT.md` (ver formato no próprio arquivo).
