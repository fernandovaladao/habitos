#!/bin/sh
set -eu

tracked_files=$(git ls-files | grep -Ev '^(scripts/check-secrets\.sh|go\.sum)$')
if [ -z "$tracked_files" ]; then
  exit 0
fi

if printf '%s\n' "$tracked_files" | xargs grep -EnI \
  -e '-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----' \
  -e 'AIza[0-9A-Za-z_-]{35}' \
  -e 'sk-(proj-)?[0-9A-Za-z_-]{20,}' \
  -e 're_[0-9A-Za-z_-]{20,}'; then
  echo 'Possível segredo real encontrado em arquivo versionado.' >&2
  exit 1
fi
