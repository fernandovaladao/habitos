# ADR 0011 — Ranking geral e projeção pública

- Status: Aceito
- Data: 2026-08-22

## Contexto

O ranking combina a pontuação acumulada da gamificação com dados mínimos de apresentação. Como o perfil contém informações pessoais e o público-alvo inclui adolescentes, consultar diretamente documentos `users` aumentaria o risco de divulgação acidental e dificultaria a remoção imediata no opt-out.

## Decisão

O MVP terá somente um ranking geral. Não haverá rankings semanais ou mensais, ligas ou divisões. O ranking será acessível apenas a usuários autenticados; “público” significa visível entre usuários do produto, não acesso anônimo.

A coleção `publicRanking/{uid}` será a única fonte de todas as consultas do ranking. Não haverá fallback para `users`. Cada documento é uma projeção explícita que contém somente apelido, avatar, total de pontos e a chave temporal de ordenação, além de timestamp técnico de atualização. O UID é usado como ID do documento e desempate técnico, mas nunca é serializado ou exibido. E-mail, idade, peso, altura, gênero, timezone, hábitos, execuções, notas, streaks e preferências nunca integram a resposta pública.

A participação é exclusivamente opt-in. Um perfil válido e completo com `rankingOptIn=true` possui uma projeção; opt-out ou perfil incompleto não possui. Salvar ou garantir o perfil reconcilia a projeção idempotentemente: cria ou corrige documento ausente/divergente e remove documento indevido. Alterações de apelido e avatar atualizam perfil e projeção na mesma transação. Conforme o ADR 0013, o avatar usa um catálogo interno validado e mantém URL vazia; upload e Storage ainda não fazem parte do MVP implementado.

Quando a gamificação aumenta ou diminui o total, a mesma transação que atualiza `users/{uid}` também reconcilia `publicRanking/{uid}`. Reenvio funcional idêntico já reconciliado permanece no-op completo conforme o ADR 0009.

A ordenação canônica é:

1. total de pontos decrescente;
2. `rankingReachedAt` crescente;
3. UID crescente, usando o ID do documento.

`rankingReachedAt` copia `totalPointsReachedAt`. Quando o usuário permanece com zero pontos e `totalPointsReachedAt=nil`, a projeção usa `createdAt` como chave substituta estável; o campo original em `users` continua ausente.

A tela mostra o Top 10. Para participante opt-in, a posição é a contagem de documentos anteriores na ordenação acrescida de um e permanece visível mesmo fora do Top 10. A entrada imediatamente anterior é obtida pelo mesmo cursor. Os pontos adicionais para ultrapassá-la são:

`pontosDoAnterior - pontosDoUsuario + 1`.

O primeiro colocado não exibe distância. Usuários autenticados sem opt-in podem visualizar o Top 10, mas não recebem posição nem distância e veem CTA para ativar a participação no Perfil. Visualizar não implica consentimento.

## Consequências

- Opt-in, opt-out, alterações públicas do perfil e mudanças de pontos não deixam perfil e projeção divergentes após uma transação bem-sucedida.
- A projeção é reparável por operações normais de perfil sem infraestrutura de migração ou worker.
- Consultas de ranking não precisam ler documentos privados.
- A posição arbitrária usa agregação de contagem e cursor, evitando carregar toda a coleção.
- O Firestore Emulator não valida a necessidade de índices compostos. Por isso, o índice da consulta canônica foi versionado explicitamente para produção com `totalPoints DESC`, `rankingReachedAt ASC` e `__name__ ASC`; nenhum índice alheio às consultas atuais foi adicionado.
- A futura exclusão integral da conta deve remover a projeção no início do fluxo, conforme o ADR 0005.
