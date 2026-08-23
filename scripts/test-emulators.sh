#!/bin/sh
set -eu

export APP_ENV=development
export SESSION_COOKIE_SECURE=false
export FIREBASE_PROJECT_ID=demo-habitos-local
export GCLOUD_PROJECT=demo-habitos-local
export FIREBASE_WEB_API_KEY=public-placeholder
export FIREBASE_AUTH_DOMAIN=demo-habitos-local.firebaseapp.com
export FIREBASE_APP_ID=1:000000000000:web:placeholder
export FIREBASE_STORAGE_BUCKET=demo-habitos-local.appspot.com
export FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:9099
export FIRESTORE_EMULATOR_HOST=127.0.0.1:8081
export FIREBASE_STORAGE_EMULATOR_HOST=127.0.0.1:9199
export OPENAI_API_KEY=test-placeholder
export OPENAI_MODEL=gpt-5.6-luna
export APP_BASE_URL=http://127.0.0.1:8080
export VAPID_PUBLIC_KEY=test-public
export VAPID_PRIVATE_KEY=test-private
export VAPID_SUBSCRIBER=mailto:test@example.test
export RESEND_API_KEY=test-placeholder
export EMAIL_FROM='HÁBITOS <test@example.test>'
export REMINDER_PROCESSOR_ENABLED=true
export RUN_FIREBASE_EMULATOR_TESTS=1
export GOCACHE=${GOCACHE:-/tmp/habitos-gocache}

go test -count=1 -v ./tests/integration

cd tests/e2e
E2E_START_SERVER=1 npm test
