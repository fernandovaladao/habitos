# ADR 0017 — Funcionamento offline fora do MVP

- **Status:** Aceita
- **Data:** 2026-08-23

## Contexto

A fundação técnica inclui manifest, service worker e cache de assets estáticos. A ERS anterior tratava PWA como requisito funcional e exigia cache da interface e do conteúdo estático, embora as operações mutáveis offline já estivessem fora do escopo. Essa formulação criava uma expectativa de disponibilidade offline que não integra mais o MVP.

## Decisão

O MVP exige uma aplicação web responsiva, mas não exige funcionamento ou disponibilidade offline, inclusive para navegação, páginas, conteúdo, criação, edição ou registro.

O manifest, o service worker e o cache estático existentes serão preservados. Eles são detalhes técnicos úteis para Web Push, instalação quando suportada pelo navegador e carregamento de assets, mas não constituem promessa, requisito funcional ou critério de aceite de funcionamento offline. Não haverá nesta decisão expansão nem remoção desses recursos.

Uma futura experiência offline exigirá nova decisão explícita sobre escopo, dados disponíveis, autenticação, sincronização, conflitos, privacidade e critérios de aceite.

## Consequências

- O RF-032 e a obrigação de cache offline são removidos da ERS do MVP.
- Ausência de conexão pode impedir qualquer navegação ou operação sem caracterizar defeito do MVP.
- O suporte técnico existente não deve ser descrito na interface ou documentação como modo offline garantido.
- Nenhuma funcionalidade ou asset do repositório é alterado por esta decisão.
