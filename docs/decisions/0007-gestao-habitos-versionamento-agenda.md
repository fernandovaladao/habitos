# ADR 0007 — Gestão de hábitos e versionamento de agenda

- Status: Aceito
- Data: 2026-08-22

## Contexto

A agenda de um hábito pode mudar, mas a ERS determina que a alteração só vale a partir do dia seguinte no timezone IANA do usuário e nunca altera o histórico. Arquivamento, reativação, duplicação e exclusão também precisam preservar a integridade necessária às futuras fases de execuções, pontuação e sequência.

## Decisão

O hábito pertence exclusivamente ao UID autenticado obtido da sessão Firebase validada. O UID persistido nunca é aceito do cliente. Todas as leituras e mutações verificam o proprietário, inclusive dentro das transações Firestore.

O documento `habits/{habitId}` mantém uma projeção da configuração vigente ou futura usada pelas telas comuns. Cada configuração de agenda é também gravada como snapshot imutável em `habits/{habitId}/scheduleVersions/{versionId}`. A atualização da projeção e a criação do snapshot acontecem na mesma transação.

Na criação, o primeiro snapshot vigora no início do dia local atual. Alterações de dias, horário, meta semanal ou canal de lembrete criam um snapshot cuja vigência começa no início do dia seguinte, calculado no timezone IANA do perfil. A projeção mantém a agenda anterior enquanto necessário para resolver o dia corrente sem consultar o histórico.

Existe no máximo uma versão pendente por hábito e instante de vigência. O documento principal mantém o identificador dessa versão pendente, derivado de forma determinística do instante de vigência dentro da subcoleção do hábito. Se outra edição for feita antes de ela entrar em vigor e tiver a mesma data de vigência, a transação substitui o conteúdo do mesmo documento de versão, preserva como agenda vigente de hoje a configuração anterior e mantém como agenda de amanhã somente a última escolha do usuário. O ID determinístico e a releitura transacional impedem versões concorrentes inclusive quando duas requisições partem do mesmo estado anterior. Quando uma versão entra em vigor, ela se torna histórica e imutável; uma edição posterior usa o instante de vigência seguinte, cria um novo ID de versão e nunca reutiliza ou altera o snapshot já vigente.

Arquivar interrompe imediatamente futuras ocorrências e lembretes e preserva os snapshots. Reativar muda o status para ativo imediatamente, registra `reactivatedAt`, mas define `occurrencesResumeAt` para o início do dia seguinte no timezone do usuário. Nenhuma ocorrência pode ser considerada antes desse marco; a futura sequência começa em zero.

Excluir um hábito é uma operação lógica por `deletedAt`. O hábito deixa de aparecer nas consultas normais, não pode ser reativado no MVP e não produz ocorrências ou lembretes futuros. O documento e todos os `scheduleVersions` são preservados.

Duplicar sempre cria um novo hábito ativo com ID não previsível, novas datas e novo snapshot inicial. A configuração é copiada mesmo quando a origem está arquivada.

Quantidade da meta, peso em kg e altura em cm usam representação inteira em centésimos para evitar imprecisão binária. Peso, altura, idade e gênero pertencem somente ao perfil e nunca são persistidos no hábito.

## Consequências

- Consultas normais precisam excluir documentos com `deletedAt`.
- A atualização da projeção e a criação ou substituição da única versão pendente são atômicas no Firestore e repetem a verificação de proprietário dentro da transação.
- Processadores futuros de ocorrências e lembretes devem respeitar status, `deletedAt`, `occurrencesResumeAt` e a versão de agenda vigente na data local.
- A exclusão integral da conta continua seguindo o ADR 0005 e deverá remover também hábitos logicamente excluídos e seus snapshots.
