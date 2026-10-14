#!/usr/bin/env bash
set -euo pipefail

# Use disposable MySQL 8 and Redis services. No Feishu credentials are needed;
# outbound cards are captured by test clients and never sent to a real tenant.
: "${FEISHU_MENTION_MYSQL_DSN:?Set a MySQL 8 DSN whose database name contains _test or integration}"
: "${FEISHU_MENTION_REDIS_ADDR:?Set the dedicated integration Redis host:port}"

cd "$(dirname "$0")/.."
result_file=$(mktemp)
trap 'rm -f "$result_file"' EXIT
go test -mod=readonly -tags=integration -race -shuffle=on -count=1 -timeout=5m -json \
  ./internal/service/data_svc ./connector/feishu ./bootstrap \
  -run 'Test(FeishuMention|MentionSendLimiter|SchemaMigrationVersionIncludesFeishuMention)' | tee "$result_file"

# A missing service configuration must not produce a falsely green CI run.
if grep -q '"Action":"skip"' "$result_file"; then
  echo 'Feishu acceptance tests were skipped; verify the integration services.' >&2
  exit 1
fi
