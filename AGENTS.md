# AGENTS.md — Projeto HÁBITOS

## Fonte de verdade
Antes de implementar ou modificar qualquer funcionalidade:
1. Leia `docs/ERS_HABITOS_MVP.md`.
2. Consulte a imagem correspondente em `docs/prototypes/`.

A ERS prevalece em regras e comportamento. Os protótipos prevalecem como referência visual compatível.

## Stack obrigatória do MVP
- Go (Golang)
- HTML + CSS + JavaScript leve; HTMX é permitido quando simplificar
- PWA responsiva
- Google Cloud Run
- Cloud Firestore
- Firebase Authentication
- Cloud Storage/Firebase Storage
- API de IA somente via backend
- Git + GitHub

## Regras centrais
- Interface em Português do Brasil.
- Os 4 Rs são exclusivamente: Relembrar, Rotina, Recompensa e Repetição.
- Não usar os 4 Rs ambientais.
- Não adicionar administrador no MVP.
- Ranking: somente geral; sem ligas, divisões, ranking semanal ou mensal.
- Ranking público: apenas apelido, avatar e pontuação.
- Não retirar pontos.
- Meta simples concluída = 10 pontos.
- Meta quantitativa parcial: 10 × (realizado/meta), arredondado para inteiro, máximo 10.
- Sequência = execuções programadas consecutivas.
- Bônus: 3=+10, 7=+25, 15=+50, 30=+100.
- O termo de interface é `Notificação`, não `Push notification`.
- Nunca expor segredos, chaves ou credenciais no frontend ou repositório.
- Usar variáveis de ambiente.
- Escrever testes para pontuação, sequência, bônus, autorização e privacidade.

## Navegação principal
`Início | Criar Hábito | Meus Hábitos | Progresso | Recompensas | Aprenda os 4 Rs | Perfil`

## Fluxo de trabalho
1. Localizar requisitos na ERS.
2. Consultar o protótipo aplicável.
3. Implementar a menor solução completa do MVP.
4. Validar entradas no servidor.
5. Atualizar testes.
6. Executar testes.
7. Documentar decisões técnicas não previstas.

## Não fazer sem aprovação
- ampliar escopo;
- trocar Go/Firestore/Firebase Auth/Cloud Run;
- adicionar framework frontend pesado;
- criar recursos sociais/chat;
- mudar fórmula de pontuação;
- mudar marcos de sequência;
- introduzir rankings adicionais.
