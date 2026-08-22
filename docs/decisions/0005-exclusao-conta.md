# ADR 0005 — Exclusão de conta e dados

- **Status:** Aceita
- **Data:** 2026-08-22

## Contexto

Os dados de uma conta estão distribuídos entre Firebase Authentication, Firestore, Cloud Storage e a projeção de ranking. Esses serviços não oferecem uma transação única, e uma falha intermediária não pode deixar o usuário publicamente visível ou impedir a retomada da exclusão.

## Decisão

No MVP, excluir conta significa excluir, e não anonimizar, todos os dados pertencentes ao usuário, incluindo no mínimo perfil, hábitos, execuções, streaks/sequências, notas, desbloqueios de conquistas, preferências, projeção de ranking, registros de lembretes associados e fotos/objetos de Storage. O usuário também deve ser removido do ranking. A ação exige confirmação explícita.

O fluxo de exclusão será idempotente e recuperável. Repetir uma etapa já concluída deve ser seguro, e a remoção da exposição pública deve ocorrer no início do processo. A conta do Firebase Authentication será removida como parte do fluxo coordenado pelo backend.

Logs do processo poderão registrar identificadores técnicos e estado das etapas, mas não conteúdo de notas, hábitos, tokens, credenciais ou outros dados sensíveis.

## Consequências

- A implementação deverá controlar o estado do processo e permitir nova tentativa após falha parcial.
- Referências e objetos de Storage precisam ser removidos explicitamente.
- Índices, projeções e agregados não podem manter o usuário no ranking após o início confirmado da exclusão.
- Testes devem cobrir repetição do processo e falhas em etapas intermediárias.
