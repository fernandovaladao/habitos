# HÁBITOS

MVP de uma PWA para adolescentes, focada na formação e manutenção de hábitos positivos usando os **4 Rs da formação de hábitos**: **Relembrar, Rotina, Recompensa e Repetição**.

## Objetivo
Permitir criar hábitos, definir metas e lembretes, registrar execuções, acompanhar progresso, receber pontos/conquistas e visualizar um ranking geral com privacidade.

## Stack aprovada
- Backend: Go (Golang)
- Frontend: HTML + CSS + JavaScript leve; HTMX quando simplificar
- PWA responsiva
- Hospedagem: Google Cloud Run
- Banco: Google Cloud Firestore
- Autenticação: Firebase Authentication
- Fotos: Cloud Storage/Firebase Storage
- IA: API acessada somente pelo backend Go
- Versionamento: Git + GitHub

## Documentação
- `docs/ERS_HABITOS_MVP.md`: especificação formal
- `AGENTS.md`: instruções operacionais para agentes de código
- `docs/prototypes/`: referências visuais regeneradas e aprovadas conceitualmente

## Regra de precedência
A ERS é fonte de verdade para comportamento e regras de negócio. Os protótipos são referência visual. Em conflito funcional, prevalece a ERS.
