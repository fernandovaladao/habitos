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
- `docs/decisions/`: decisões arquiteturais aceitas

## Regra de precedência
A ERS é fonte de verdade para comportamento e regras de negócio. Os protótipos são referência visual. Em conflito funcional, prevalece a ERS.

## Fundação técnica atual

Esta etapa contém somente a fundação do monólito modular: servidor HTTP em Go, templates renderizados no servidor, assets incorporados ao binário, navegação básica, PWA estática e endpoint de saúde. Firebase, Firestore, hábitos, pontuação, ranking, notificações e IA ainda não estão integrados.

## Requisitos locais

- Go 1.24 ou superior.
- Docker, opcional, para validar a imagem de produção.

O projeto usa apenas a biblioteca padrão do Go nesta etapa.

## Executar localmente

```bash
go run ./cmd/web
```

O servidor usa a porta informada pela variável de ambiente `PORT`; na ausência dela, usa `8080`.

```bash
PORT=3000 go run ./cmd/web
```

Acesse [http://localhost:8080](http://localhost:8080) e verifique a saúde em [http://localhost:8080/health](http://localhost:8080/health).

## Testes

```bash
go test ./...
```

## Docker

```bash
docker build -t habitos .
docker run --rm -p 8080:8080 -e PORT=8080 habitos
```

O container é compatível com o contrato do Cloud Run: escuta em `0.0.0.0` na porta definida por `PORT` e encerra de forma graciosa ao receber `SIGTERM`.
