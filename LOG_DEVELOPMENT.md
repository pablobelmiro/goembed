# Diário de desenvolvimento — goembed

Diário de sessão, em ordem cronológica (mais recente no final). Cada
entrada deve bastar para retomar o trabalho sem depender de memória de
conversa — leia também `ARQUITETURA_OFICIAL.md` (fonte de verdade) e
`CLAUDE.md` (regras operacionais) antes de continuar.

**Formato de entrada:**
```
## AAAA-MM-DD — título curto

**Contexto:** por que essa sessão aconteceu / o que motivou.
**Feito:** o que foi de fato produzido ou verificado.
**Decisões:** o que foi fechado, com o porquê.
**Pendências:** o que ficou em aberto, e o que destrava.
**Próximo passo:** a ação concreta da próxima sessão.
```

---

## 2026-08-26 — Da ideia ao J0 fechado: arquitetura, spikes e harness

**Contexto:** Pablo trouxe `plano_golang_lib.md` — um plano tático já
escrito para o Épico 1 (fronteira FFI zero-CGO/ONNX Runtime via purego) —
e pediu planejamento antes de qualquer implementação. O diretório do
projeto estava vazio além desse arquivo; nenhum código, nenhum git.

**Feito:**

- Brainstorming completo (caminho arquitetural): produto definido como
  pipeline de embeddings de texto (`feature-extraction`), zero-CGO no
  build, distribuição do `.so` por caminho explícito/env var (nunca
  `go:embed` nem download automático), modelo lido do disco (download
  fica em subpacote `hub` opcional).
- **Dois spikes descartáveis** rodados antes de escrever qualquer design
  em cima de suposição:
  - Cadeia `dlopen → OrtGetApiBase → GetApi → CreateEnv → Run → Release*`
    via `purego`, `CGO_ENABLED=0`, contra `libonnxruntime.so.1.24.3` real
    — **confirmada**, incluindo o caminho de erro (`checkStatus`).
  - Tokenizer WordPiece/BERT em Go puro (`BertNormalizer` +
    `BertPreTokenizer` + `WordPiece` + `TemplateProcessing` +
    `added_tokens`), comparado token-a-token contra `tokenizers` 0.22.2
    (Rust, oficial) — **23/23 idêntico**, corpus adversarial com
    acentuação PT-BR, CJK, emoji, contrações.
- Duas correções factuais ao `plano_golang_lib.md`: `ORT_API_VERSION`
  real é **24** para a 1.24.3 (o plano cravava 20), e `VerifyOffsets()`
  lia `GetVersionString` do offset errado (`OrtApi` em vez de
  `OrtApiBase`).
- Pesquisa de mercado (agente em background) **revogou duas premissas**
  da proposta de valor original: `hugot` já tem modo zero-CGO no build
  padrão, e `CGO_ENABLED=0` com purego **não** produz binário estático
  (ainda depende de `libdl.so.2`). Proposta de valor reformulada para
  "velocidade nativa do ORT sem CGO no build" — o nicho real, confirmado
  vazio (o único prior art, `shota3506/onnxruntime-purego`, trava em
  API 23 e se declara instável).
- Três críticas técnicas do Pablo incorporadas: `runtime.Pinner`
  obrigatório para tensores retidos pelo ORT (não `KeepAlive` sozinho);
  invariante único de propriedade de memória resolve `AllocatorFree` sem
  acoplamento; isolamento de dois níveis para o artefato de teste
  (sintético vs. MiniLM real).
- **J0 fechada** (as quatro decisões que bloqueavam a Janela 1):
  - §6.1 — gerador de offsets emite wrappers via `purego.RegisterFunc`
    (não `SyscallN` cru — este é comprovadamente incorreto para
    assinaturas com float).
  - §6.3 — Apache-2.0, módulo `github.com/pablobelmiro/goembed`.
  - §6.6 — tokenizer **construído**, não importado (justificativa:
    contribuição própria pesa mais para a banca do PPComp/IFES Serra do
    que reusar um módulo pronto, ainda que mais completo).
  - §6.7 — ONNX Runtime pinado em **1.28.0** (alinhado ao que o `hugot`
    usa em produção).
- Repositório git inicializado e primeiro commit feito
  (`7a8a4bb` — os dois documentos).
- **Pendência do §6.7 resolvida**: baixado o release oficial
  `onnxruntime-linux-x64-1.28.0.tgz`, para
  `~/.cache/goembed/onnxruntime/1.28.0/` (fora do repo). Spike
  zero-CGO reexecutado contra o binário real 1.28.0 — confirmado. Achado
  extra: testado o contrato exato do `GetApi` (aceita `versão ≤ versão do
  runtime`, devolve sempre a struct atual e completa — não uma versão
  histórica truncada); `GetApi(29)` contra o runtime 1.28.0 falha com a
  mensagem do próprio ORT, confirmando o limite.
- Harness para agentes: `CLAUDE.md` do projeto criado (zona de alto
  escrutínio do `ortcore`, convenções de teste, disciplina de commit por
  janela). Decidido não adicionar `.claude/settings.local.json` com deny
  técnico por enquanto — só orientação documental.
- Política nova: Go e dependências sempre na **última versão estável**
  publicada (nunca beta/RC/`main`) — `go.mod` deve declarar
  `go 1.27.0` + `toolchain go1.27.0` (estável mais recente verificada em
  `go.dev/dl` nesta data; o `go` instalado localmente é 1.26.2, atrasado).

**Decisões registradas em `ARQUITETURA_OFICIAL.md`:** §1.1 (revisada),
§2.6 (pesquisa de mercado), §3.4a (versões sempre estáveis), §3.5/§3.6
(memória e pinning), §6.1/§6.3/§6.6/§6.7 (J0), §6.7.1 (artefato
adquirido e verificado), §7 (plano tático em 7 janelas).

**Pendências:**
- Atualizar o `go` local para 1.27.0 (o `toolchain` directive cobre isso
  automaticamente na primeira build, mas não substitui atualizar o
  binário).
- Nenhum `go.mod`/código de produto existe ainda — a J1
  (`internal/ortgen`) não foi iniciada.

**Próximo passo:** invocar o skill `writing-plans` para transformar a J1
da §7 num plano de implementação detalhado (arquivos, ordem, critérios de
teste), só então começar a escrever código.

---

## 2026-08-27 — J1: internal/ortgen fechada

**Contexto:** primeira janela de código do projeto, seguindo o plano em
`docs/plans/2026-08-26-j1-ortgen.md`.

**Feito:**
- `go.mod` bootstrapped (`github.com/pablobelmiro/goembed`, Go 1.27.0).
- `internal/ortgen`: gerador que compila `dump_offsets.c` contra o
  header pinado, executa e emite `ortcore/ortapi_gen.go`. Assinatura C de
  cada uma das ~25 funções do steel thread é verificada pelo compilador
  (`__typeof__` + `__builtin_types_compatible_p`, nunca executado); a
  aridade do tipo Go correspondente é cross-checada contra essa mesma
  assinatura C em `main.go` (achado Important 1 da revisão final de
  branch — o lado Go era digitado à mão sem checagem nenhuma até esta
  correção). Provado, com dois testes, que uma assinatura C divergente
  falha a compilação do gerador (não corrompe silenciosamente um
  offset) e que um tipo Go divergente (aridade errada) falha a
  checagem de aridade em `generate()`.
- `ortcore/ortapi_gen.go` gerado e commitado, com teste de regressão
  contra os offsets já verificados em `ARQUITETURA_OFICIAL.md` §2.2.
- `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...` passa
  para o módulo inteiro.

**Decisões:** nenhuma nova — execução do que já estava fechado em J0
(§6.1).

**Pendências:** nenhuma para J1. `ortcore.go` (struct `api`, `Load()`,
`checkStatus`) ainda não existe — é a J2.

**Próximo passo:** planejar J2 (`ortcore`: carga e erros) via
`writing-plans`.

---

## 2026-08-27 — J1: rodada final de correção (revisão de branch inteiro)

**Contexto:** a revisão final da J1 inteira (revisor sênior, reprodução
independente empírica) encontrou 3 achados Important e 8 Minor. Veredito
"Ready to merge — With fixes"; nenhum justificava reabrir a janela.
(Relatório completo vivia em `.superpowers/sdd/`, workspace do plano —
apagado ao fechar a J1, como o processo determina; o resumo abaixo e o
commit `a1b7acd` são o registro permanente.)

**Feito:**
- **Important 1** (o mais delicado): a verificação de assinatura só
  cobria o lado C; `GOSIG` era digitada à mão sem checagem — provado
  mutável sem erro. Fechado: `dump_offsets.c` agora emite a assinatura C
  como comentário `// C: ...` acima de cada tipo Go, e `generate()` em
  `internal/ortgen/main.go` cross-checa aridade dos dois lados. Novo
  teste `TestGenerate_DivergentGoSignatureFailsArityCheck` prova o
  fechamento. `ortcore/ortapi_gen.go` regenerado.
- **Important 2:** erros de `cc`/do binário gerado agora incluem o `err`
  original (e `Stderr` de `*exec.ExitError`), não só a saída capturada.
- **Important 3:** `ARQUITETURA_OFICIAL.md` atualizada — §5.1 marcada
  `[RESOLVIDO]`, §7 marca J1 fechada, cabeçalho e §8 em v0.6, nota sobre
  o padrão de emissão de offsets (constantes individuais, não bloco
  `const (...)`, por causa do realinhamento do `go/format`).
- **Minors:** `gofmt` aplicado e `TestSteelThreadOffsets` reestruturado
  (slice de structs, cobre todos os offsets); comentário de topo de
  `dump_offsets.c` traduzido, sem referência a "Task 1-3"; `ORT_HEADER_DIR`
  documentado em `ortcore/generate.go`; `$CC` respeitado com fallback
  `cc`; cabeçalho gerado nomeia a versão do ORT; frase deste diário
  (entrada anterior) corrigida para mencionar o cross-check de aridade;
  `.gitignore` criado (`/ortgen`, `.claude/`, `.superpowers/`).

**Decisões:** nenhuma nova — só correções ao já fechado em J1/§6.1.

**Pendências:** nenhuma. `ortcore.go` (J2) segue não iniciado.

**Próximo passo:** planejar J2 via `writing-plans`, como já registrado
acima.

---

## 2026-08-27 — J1: re-revisão final aprovada, push, pausa

**Contexto:** depois da leva de correção acima, uma re-revisão da leva
inteira (também de branch inteiro, modelo mais capaz) reproduziu de
forma independente cada um dos 3 Important + 8 Minor — inclusive
mutando assinaturas de novo, com funções diferentes das que o
implementador tinha usado, e testando casos de borda do checador de
aridade (`(void)`, parâmetro único sem vírgula, ponteiro de função
aninhado, entrada malformada). Todos os 11 vieram **ADDRESSED**,
nenhuma quebra nova. Essa era a última rodada de correção automática
permitida pelo processo — o que sobrasse seria adjudicado por mim, não
corrigido de novo.

**Feito:**
- 5 achados residuais (R1-R5) levantados pela própria re-revisão,
  todos adjudicados nesta sessão (nenhum bloqueia a J2):
  - **R1 (parked, real):** o guarda de aridade Go que a leva anterior
    construiu é ele mesmo desguarnecido — se as linhas `// C: ...`
    sumirem de `dump_offsets.c`, `checkGoSignatureArity` não acha
    nada, não erra, e a suíte de testes atual não percebe (só conta
    ocorrências de `"type fn"`, nunca de `"// C: "`). Provado
    removendo o `printf` do comentário e mutando `Run` ao mesmo
    tempo — passou limpo. Correção recomendada, barata: um `assert`
    contando `// C: ` == 25 dentro de `checkGoSignatureArity` (ou como
    asserção extra em `TestGenerate_AgainstRealHeader`).
  - **R2 (parked, baixa severidade):** `fnGetApi` e
    `fnGetVersionString` (os 2 tipos de `OrtApiBase`, fora do X-macro)
    não recebem comentário `// C: ` — corretos hoje por inspeção, não
    por construção.
  - **R3 (sem ação, por design):** o guarda checa só aridade, não
    tipo (`int32`↔`float64` passaria). Já documentado no código como
    escopo intencional; um erro desse tipo vira bug de runtime na J2
    (`purego.RegisterFunc`), não de build.
  - **R4 (parked, cosmético — corrigido nesta mesma edição):** a
    entrada anterior deste diário apontava para
    `.superpowers/sdd/2026-08-26-j1-ortgen/final-fix-report.md`, path
    do workspace do plano, já removido ao fechar a J1 (ver nota
    editada acima). Corrigido para não apontar mais para um arquivo
    que não existe.
  - **R5 (sem ação, já mitigado):** a versão do ORT no cabeçalho
    gerado é uma string separada de `ortAPIVersion`; um bump de patch
    (1.28.0→1.28.1) não quebraria `TestOrtAPIVersionIsPinned` (só
    checa a versão de API). Já há comentário no código avisando para
    atualizar os dois juntos — não vale um mecanismo extra por uma
    string cosmética de patch.
- Workspace do plano (`.superpowers/sdd/2026-08-26-j1-ortgen/`)
  apagado — git-ignored desde o começo, o histórico do git é o
  registro permanente a partir daqui.
- `git push -u origin master` feito com sucesso para
  `git@github.com:pablobelmiro/goembed.git` (repo remoto estava vazio,
  sem conflito). Branch `master` local agora rastreia `origin/master`.

**Decisões:** nenhuma nova de arquitetura — só as 6 rulings de processo
desta janela (aceitar a correção do `go.mod`; aceitar a data real da
sessão sobre a data literal do brief; e as 4 acima, R1/R2/R4 parked,
R3/R5 sem ação).

**Pendências:** R1, R2 e R4 seguem reais mas não bloqueiam a J2 —
candidatos naturais para virar a primeira tarefa da J2, ou uma janela
de limpeza rápida antes dela. Nenhuma outra pendência da J1.

**PAUSADO AQUI, a pedido do Pablo.** A J1 está fechada, revisada,
documentada e no remote. Para retomar exatamente deste ponto: ler este
diário (esta entrada) + `ARQUITETURA_OFICIAL.md` (v0.7, ver §8) — não
depender de memória de conversa. Próximo passo real: decidir se R1/R2/R4
entram antes da J2 ou junto com ela, depois planejar a J2
(`ortcore`: carga e erros) via `writing-plans`.
