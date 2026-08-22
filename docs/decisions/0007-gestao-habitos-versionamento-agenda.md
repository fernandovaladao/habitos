# ADR 0007 — Gestão de hábitos e versionamento de agenda

- Status: Aceito
- Data: 2026-08-22

## Contexto

A agenda de um hábito pode mudar, mas a ERS determina que a alteração só vale a partir do dia seguinte no timezone IANA do usuário e nunca altera o histórico. Arquivamento, reativação, duplicação e exclusão também precisam preservar a integridade necessária às futuras fases de execuções, pontuação e sequência.

## Decisão

O hábito pertence exclusivamente ao UID autenticado obtido da sessão Firebase validada. O UID persistido nunca é aceito do cliente. Todas as leituras e mutações verificam o proprietário, inclusive dentro das transações Firestore.

O documento `habits/{habitId}` mantém uma projeção da configuração vigente ou futura usada pelas telas comuns. Cada configuração de agenda é também gravada como snapshot imutável em `habits/{habitId}/scheduleVersions/{versionId}`. A atualização da projeção e a criação do snapshot acontecem na mesma transação.

Cada versão possui `effectiveDate` canônico em `YYYY-MM-DD`. A seleção da versão aplicável a uma ocorrência usa a maior `effectiveDate` menor ou igual a `scheduledDate`, preservando semântica de data civil mesmo após uma mudança de timezone. `effectiveAt` permanece como informação complementar de auditoria, não como chave de seleção.

Na criação, o primeiro snapshot vigora na data local atual. Alterações de dias, horário, meta semanal, canal de lembrete ou meta criam um snapshot cuja vigência civil começa no dia seguinte. A projeção mantém a configuração anterior para o dia corrente e a última configuração escolhida para o dia seguinte.

Existe no máximo uma versão pendente por hábito e data civil de vigência. O documento principal mantém o identificador dessa versão pendente, derivado de forma determinística de `effectiveDate` dentro da subcoleção do hábito. Se outra edição for feita antes de ela entrar em vigor e tiver a mesma data de vigência, a transação substitui o conteúdo do mesmo documento de versão, preserva como configuração vigente de hoje a versão anterior e mantém como configuração de amanhã somente a última escolha do usuário. O ID determinístico e a releitura transacional impedem versões concorrentes inclusive quando duas requisições partem do mesmo estado anterior. Quando uma versão entra em vigor, ela se torna histórica e imutável; uma edição posterior usa a data de vigência seguinte, cria um novo ID de versão e nunca reutiliza ou altera o snapshot já vigente.

Arquivar interrompe imediatamente futuras ocorrências e lembretes e preserva os snapshots. Reativar muda o status para ativo imediatamente, registra `reactivatedAt`, `occurrencesResumeAt` e a data civil canônica `occurrencesResumeDate` correspondente ao dia seguinte no timezone vigente. A geração futura compara datas civis com `occurrencesResumeDate`; uma mudança posterior de timezone não reinterpreta a retomada já estabelecida. Nenhuma ocorrência pode ser considerada antes desse marco; a futura sequência começa em zero.

Excluir um hábito é uma operação lógica por `deletedAt`. O hábito deixa de aparecer nas consultas normais, não pode ser reativado no MVP e não produz ocorrências ou lembretes futuros. O documento e todos os `scheduleVersions` são preservados.

Duplicar sempre cria um novo hábito ativo com ID não previsível, novas datas e novo snapshot inicial. A configuração é copiada mesmo quando a origem está arquivada.

Quantidade da meta, peso em kg e altura em cm usam representação inteira em centésimos para evitar imprecisão binária. Peso, altura, idade e gênero pertencem somente ao perfil e nunca são persistidos no hábito.

## Consequências

- Consultas normais precisam excluir documentos com `deletedAt`.
- A atualização da projeção e a criação ou substituição da única versão pendente são atômicas no Firestore e repetem a verificação de proprietário dentro da transação.
- Processadores futuros de ocorrências e lembretes devem respeitar status, `deletedAt`, `occurrencesResumeAt` e a versão de agenda vigente na data local.
- A exclusão integral da conta continua seguindo o ADR 0005 e deverá remover também hábitos logicamente excluídos e seus snapshots.
