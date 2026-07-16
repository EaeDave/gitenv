# gitenv

<!-- business-readme:business-rules:start -->
## Regras do produto

`gitenv` mantém perfis criptografados de arquivos `.env` e usa Git apenas para versioná-los e transportá-los entre computadores.

- A interface principal é a TUI aberta pelo comando `gitenv`; no primeiro uso ela cria ou clona o vault e conduz recovery, remoto e cadastro do projeto atual.
- Quando executado dentro de um repositório Git, `gitenv` usa a raiz do repositório e detecta automaticamente seu `.env`; fora de Git, usa o diretório atual.
- A TUI permite cadastrar o projeto atual, criar/aplicar/capturar/remover perfis, sincronizar o vault e exportar recovery sem exigir comandos auxiliares.
- Se o vault configurado tiver sido apagado ou estiver temporariamente indisponível, a TUI volta ao onboarding somente para a sessão atual; o caminho salvo não é apagado até um novo vault ser criado ou clonado.
- Cada projeto pode ter perfis nomeados, como `dev`, `staging` e `prod`.
- Um perfil ativo não pode ser removido; aplique outro perfil antes. Perfis inativos exigem confirmação explícita e a remoção elimina somente seu ciphertext e metadados do vault.
- `capture` salva o `.env` atual no perfil indicado; comentários, linhas desativadas, ordem e quebras de linha são preservados byte a byte.
- `switch` aplica um perfil ao `.env` do projeto vinculado neste computador.
- Um `.env` modificado depois da última captura nunca é sobrescrito silenciosamente. A CLI exige `--force`; a TUI exige confirmação explícita.
- `pull` atualiza somente o vault criptografado. Ele não altera os `.env` dos projetos.
- O Git armazena somente ciphertext e metadados. A identidade privada fica fora do vault e deve ter backup separado.
- Caminhos de projetos são locais por computador e nunca são enviados ao repositório.
- Perder todas as cópias da identidade torna os perfis irrecuperáveis.
<!-- business-readme:business-rules:end -->

<!-- business-readme:technical:start -->
## Guia técnico

### Requisitos

- Git instalado e autenticado para o remoto escolhido.
- Go 1.24+ somente para compilar do código-fonte.

### Compilar

```bash
go build -o gitenv ./cmd/gitenv
```

### Uso principal

Entre em um projeto que já tenha `.env` e execute:

```bash
cd ~/dev/minha-api
gitenv
```

No primeiro uso, a TUI guia todo o processo:

1. criar um vault novo ou clonar um existente;
2. exportar ou importar a recovery identity;
3. configurar um remoto Git opcional;
4. detectar o projeto e `.env` atuais;
5. cadastrar o projeto e capturar o perfil inicial.

Depois, a tela principal permite aplicar/capturar perfis, criar perfis, executar pull/push, configurar remoto e exportar novo backup da recovery identity.

Atalhos principais:

```text
enter  abrir projeto ou aplicar perfil
a      adicionar projeto atual
c      capturar perfil ativo
n      criar perfil (dentro do projeto)
p      pull do vault
u      push do vault
g      configurar remoto Git
b      exportar recovery identity
r      recarregar
q      voltar ou sair
```

O comando `gitenv pull` atualiza somente o vault; arquivos `.env` locais continuam sendo aplicados explicitamente pela tela de perfis. A CLI permanece disponível para automação e recuperação.

### Verificação

```bash
go test ./...
go build ./...
```
<!-- business-readme:technical:end -->
