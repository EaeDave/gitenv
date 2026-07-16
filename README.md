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
- `capture` criptografa o `.env` atual preservando comentários, linhas desativadas, ordem e quebras de linha byte a byte. Antes de qualquer captura, a TUI mostra um preview estrutural com nomes e tipos de mudança, nunca valores, e só grava após confirmação explícita; a CLI permanece direta para automação. `switch` aplica um perfil ao projeto vinculado.
- Um `.env` modificado depois da última captura nunca é sobrescrito silenciosamente: a CLI exige `--force`; a TUI exige confirmação explícita.
- Um perfil ativo não pode ser removido; aplique outro primeiro. Remover perfil inativo exige confirmação e elimina apenas ciphertext e metadados do vault.
- Na tela de perfis, apenas o perfil ativo pode aparecer como `modified` (em amarelo), porque o único `.env` local corresponde a ele; os demais perfis são snapshots íntegros no vault e, quando o `.env` local coincide byte a byte com um deles, esse perfil é sinalizado como `matches .env`. O status do projeto também é destacado em amarelo quando há mudança não capturada.
- A TUI distingue o estado do `.env` em relação ao perfil ativo do estado do vault local em relação ao remoto. A verificação remota faz apenas `fetch`, expira após oito segundos e informa sincronizado, mudanças locais, mudanças remotas, divergência, falta de remoto, indisponibilidade ou falha de autenticação.
- O painel de sincronização mostra automaticamente um resumo semântico do vault antes de qualquer ação. O atalho `v` abre o visualizador completo e rolável, incluindo diferenças entre cada `.env` local e seu perfil ativo, mudanças do vault já commitadas, ainda não commitadas e recebidas do remoto. Por padrão, as visualizações exibem somente projeto, perfil, nomes de variáveis e tipos de mudança. Dentro do visualizador, `x` revela sob demanda um diff literal com linhas `-` antigas e `+` novas, incluindo valores; `x` novamente oculta, e sair da tela descarta o plaintext carregado da memória do modelo.
- Dentro do visualizador é possível agir sobre um único ambiente: `Tab`/`Shift+Tab` selecionam o projeto/perfil, `p` captura e publica somente aquele `.env` e `d` restaura somente aquele `.env` pelo perfil ativo. Publicar um único ambiente exige que o vault esteja sincronizado e limpo, e tanto publicar quanto descartar exigem confirmação explícita.
- O atalho `e`, na tela de perfis ou no visualizador, abre um editor de texto embutido na própria TUI para o `.env` local do projeto, sem depender de editores externos. Enquanto se digita, o editor mostra em tempo real um diff no estilo `git diff`, com linhas `-` antigas e `+` novas, entre o `.env` local e o perfil ativo capturado no vault; quando o projeto ainda não tem perfil capturado, ele informa que não há base de comparação. Salvar (`ctrl+s`) grava o arquivo preservando byte a byte o que não foi tocado; sair com alterações não salvas exige confirmação. O editor embutido recusa `.env` com tabulações, caracteres de controle ou bytes não-UTF-8 em vez de corrompê-los silenciosamente.
- O atalho `s` propõe a ação adequada e exige confirmação antes de baixar ou publicar. Divergências e estados indisponíveis são bloqueados com orientação; sincronizar nunca aplica um perfil nem modifica arquivos `.env` locais. Os atalhos `p` e `u` permanecem disponíveis como operações avançadas explícitas.
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

Depois, a tela principal responsiva permite aplicar/capturar/criar/remover perfis, sincronizar conforme o estado detectado, administrar o remoto do vault e exportar recovery. Em terminais largos, projetos e detalhes aparecem lado a lado; em terminais estreitos, os painéis são empilhados sem depender apenas de cores para comunicar estado.

Atalhos principais:

```text
enter  abrir projeto ou aplicar perfil
a      adicionar projeto atual
c      capturar perfil ativo
n      criar perfil (dentro do projeto)
d      remover perfil inativo (dentro do projeto)
v      abrir visualizador completo das mudanças locais e do vault
s      sincronizar conforme o estado remoto detectado
p      pull explícito do vault (avançado)
u      push explícito do vault (avançado)
g      administrar remoto do vault (trocar/testar/remover)
b      exportar recovery identity
r      recarregar
q      voltar ou sair
```

No visualizador, use `Tab`/`Shift+Tab` para selecionar o ambiente, `e` para editar o `.env` selecionado, `p` para publicar e `d` para descartar o ambiente selecionado, `x` para revelar/ocultar valores, `↑`/`↓` ou `j`/`k` para rolar, `PgUp`/`PgDn` para navegar por página, `Home`/`End` para ir ao início/fim e `Esc`/`q` para voltar. No editor, `ctrl+s` salva, `esc` cancela e `enter` cria nova linha.

O comando `gitenv pull` atualiza somente o vault; arquivos `.env` locais continuam sendo aplicados explicitamente pela tela de perfis. A CLI permanece disponível para automação e recuperação.

### Verificação

```bash
go test ./...
go build ./...
```
<!-- business-readme:technical:end -->
