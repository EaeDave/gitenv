# gitenv

<!-- business-readme:business-rules:start -->
## Regras do produto

`gitenv` mantém perfis criptografados de arquivos `.env` e usa Git apenas para versioná-los e transportá-los entre computadores.

- A interface principal é a TUI aberta pelo comando `gitenv`; no primeiro uso ela cria um vault protegido por senha mestra ou clona um vault existente.
- A senha mestra protege a identidade privada armazenada no vault. Ela deve ter ao menos 12 caracteres e nunca é salva. A identidade desbloqueada usa o cofre de credenciais do sistema quando disponível, com fallback local de permissão restrita.
- Vaults antigos sem proteção são migrados pela TUI antes de qualquer acesso. Uma identidade carregada só é aceita se for recipiente autorizado do vault; telas bloqueadas não permitem acessar o dashboard por `Esc`/`q`.
- Um computador novo pode entrar por senha mestra, colar sua recovery key diretamente na TUI ou pedir aprovação a um dispositivo autorizado. Importação por caminho de arquivo permanece disponível apenas na CLI avançada.
- Se nenhuma forma de acesso estiver disponível, a pessoa pode desconectar o vault somente deste computador e voltar ao onboarding. Isso não apaga os arquivos criptografados nem o remoto; sem senha, recovery ou dispositivo autorizado, os segredos antigos continuam criptograficamente irrecuperáveis.
- O remoto do vault é independente dos remotos dos projetos. A TUI permite configurar, trocar, testar ou remover esse remoto e nunca exibe credenciais embutidas na URL.
- Quando executado dentro de um repositório Git, `gitenv` usa sua raiz e detecta o `.env`. Após desbloquear, um remoto de projeto equivalente identifica e vincula automaticamente o projeto, mesmo com pasta renomeada ou URL SSH/HTTPS diferente; isso nunca aplica um perfil nem sobrescreve o `.env`.
- Sem correspondência exata de remoto, apenas o mesmo nome de pasta pode sugerir um projeto existente; projetos sem relação não são sugeridos.
- Cada projeto pode ter perfis nomeados, como `dev`, `staging` e `prod`. Caminhos e vínculos são locais por computador e nunca são enviados.
- `capture` criptografa o `.env` atual preservando comentários, linhas desativadas, ordem e quebras de linha byte a byte. `switch` aplica um perfil ao projeto vinculado.
- Um `.env` modificado depois da última captura nunca é sobrescrito silenciosamente: a CLI exige `--force`; a TUI exige confirmação explícita.
- Um perfil ativo não pode ser removido; aplique outro primeiro. Remover perfil inativo exige confirmação e elimina apenas ciphertext e metadados do vault.
- `pull` atualiza somente o vault criptografado; aplicar perfis locais é sempre explícito. `push` publica mudanças criptografadas com Git.
- Git armazena somente ciphertext, metadados e material público/embrulhado. Recovery identity e senha mestra exigem guarda separada; perder ambas as formas de acesso torna os perfis irrecuperáveis.
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

No primeiro uso, a TUI guia o processo completo:

1. criar um vault protegido por senha mestra ou clonar um existente;
2. configurar o remoto Git do vault, se necessário;
3. desbloquear com senha mestra, pedir aprovação a outro dispositivo ou colar a recovery key;
4. detectar e vincular o projeto atual sem sobrescrever seu `.env`;
5. capturar um perfil inicial ou aplicar explicitamente um perfil existente.

Se não houver mais nenhuma credencial, escolha **Disconnect this vault and start again**. O vínculo/configuração local será limpo, mas o diretório do vault e o remoto Git não serão apagados.

Depois, a tela principal permite aplicar/capturar/criar/remover perfis, executar pull/push, administrar o remoto do vault e exportar recovery.

Atalhos principais:

```text
enter  abrir projeto ou aplicar perfil
a      adicionar projeto atual
c      capturar perfil ativo
n      criar perfil (dentro do projeto)
d      remover perfil inativo (dentro do projeto)
p      pull do vault
u      push do vault
g      administrar remoto do vault (trocar/testar/remover)
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
