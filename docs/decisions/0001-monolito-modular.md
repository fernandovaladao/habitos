# ADR 0001 — Monólito modular em Go

- **Status:** Aceita
- **Data:** 2026-08-22

## Contexto

O MVP precisa atender páginas web responsivas, autenticação, hábitos, execuções, gamificação, ranking, lembretes, IA e integrações com serviços Google. A equipe deve manter baixo o custo operacional e evitar complexidade distribuída prematura.

## Decisão

O backend será um monólito modular em Go implantado no Cloud Run. O frontend usará HTML renderizado no servidor, CSS e JavaScript leve, com HTMX quando simplificar a interação.

Os módulos serão separados por responsabilidade de negócio. A interface HTTP não conterá regras de pontuação, sequência, bônus, autorização ou privacidade. Casos de uso coordenarão as regras, enquanto adaptadores encapsularão Firebase Authentication, Firestore, Storage, serviço de lembretes e API de IA.

Operações relacionadas a execução, pontuação, sequência, bônus e conquistas deverão ser idempotentes e usar transações do Firestore quando exigirem consistência conjunta.

Não serão criados microsserviços no MVP. Um processo de worker separado só será adotado se a solução aprovada de lembretes exigir outro ponto de entrada, sem alterar a organização modular do domínio.

## Consequências

- Implantação, observabilidade e desenvolvimento local permanecem simples.
- As fronteiras modulares permitem testes isolados e futura extração, se algum requisito posterior justificar.
- O monólito não autoriza acoplamento direto entre handlers e Firestore nem duplicação das regras de negócio.
- Segredos e chamadas à IA permanecem exclusivamente no backend.

