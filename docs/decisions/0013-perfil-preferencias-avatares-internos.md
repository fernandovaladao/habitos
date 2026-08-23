# ADR 0013 — Perfil completo, preferências e avatares internos

- Status: Aceito
- Data: 2026-08-22

## Contexto

O perfil mínimo já armazena dados pessoais, timezone e participação no ranking. A ERS também prevê pontuação, posição, preferências de lembrete e avatar. Nesta etapa não haverá entrega real de lembretes nem exclusão integral da conta. O upload privado implementado posteriormente é regido pelo ADR 0014 e não altera o avatar público do Ranking.

## Decisão

O perfil persistirá `reminderNotificationEnabled` e `reminderEmailEnabled`. Essas preferências são bloqueios mestres futuros: um hábito só poderá gerar entrega por um canal quando o próprio hábito o solicitar e a preferência global correspondente estiver habilitada. Nesta fase elas são apenas configuradas e a interface informa explicitamente que o envio ainda não está ativo. Novos perfis começam com ambos os canais habilitados.

O catálogo fechado de avatares internos usa os códigos canônicos `default`, `blue`, `orange`, `green` e `purple`. O backend valida o código; não aceita URL, caminho ou UID do cliente. Ele permanece como avatar público e fallback mesmo após a introdução da foto privada no ADR 0014.

Avatar e apelido são atualizados no documento privado `users/{uid}` e reconciliados com `publicRanking/{uid}` na mesma transação Firestore. A projeção pública continua contendo apenas apelido, avatar e pontuação, conforme o ADR 0011, e usa `avatarUrl` vazio para avatares internos.

A página de Perfil exibe `totalPoints` do documento do usuário. Para perfil completo com opt-in ativo, consulta a posição corrente na projeção pública usando a ordenação canônica do ranking; a posição não é persistida. Usuário sem opt-in não recebe posição no Perfil.

Toda leitura e mutação usa exclusivamente o UID da sessão validada e operações mutáveis permanecem protegidas por CSRF. Alterar timezone continua sincronizando primeiro as ocorrências devidas no timezone anterior, conforme os ADRs 0002 e 0008.

## Consequências

- Preferências globais não alteram a configuração histórica ou futura dos hábitos e não enviam lembretes nesta fase.
- A posição mostrada pode mudar entre requisições e nunca integra o perfil.
- O catálogo fechado evita conteúdo externo e mantém o ranking sem dependência de Storage.
- Upload de foto e exclusão integral da conta permanecem para fases posteriores.
