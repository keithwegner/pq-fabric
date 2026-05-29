#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

failures=0

report_failure() {
  echo "repo-hygiene: $*" >&2
  failures=$((failures + 1))
}

if find . -path './.git' -prune -o -path './tmp' -prune -o -path './dist' -prune -o -path './deployments/terraform/.terraform' -prune -o \( -name '.env' -o -name '.env.*' \) ! -name '*.example.env' -print | grep -q .; then
  report_failure "local .env file found outside ignored runtime folders"
fi

if find . -path './.git' -prune -o -path './tmp' -prune -o -path './dist' -prune -o \( -name 'terraform.tfstate' -o -name 'terraform.tfstate.*' -o -name '*.tfvars' -o -name '*.auto.tfvars' \) -print | grep -q .; then
  report_failure "terraform state or non-example tfvars found"
fi

if find . -path './.git' -prune -o -path './tmp' -prune -o -path './dist' -prune -o \( -name '*.pem' -o -name '*.key' -o -name '*.kubeconfig' -o -iname '*wallet*' \) -print | grep -q .; then
  report_failure "private key, kubeconfig, or wallet-looking file found"
fi

if find . -path './.git' -prune -o -path './tmp' -prune -o -path './dist' -prune -o -name 'api-keys*.json' ! -name 'api-keys.example.json' -print | grep -q .; then
  report_failure "real API key config file found; only api-keys.example.json may be committed"
fi

if find . -path './.git' -prune -o -path './tmp' -prune -o -path './dist' -prune -o -type f -size +25M -print | grep -q .; then
  report_failure "large generated-looking file over 25MB found"
fi

if [[ -d data ]]; then
  report_failure "data/ directory is present; remove generated validator state before handoff"
fi

for path in contracts/polygon/broadcast deployments/terraform/.terraform; do
  if [[ -e "$path" ]]; then
    report_failure "$path is present; remove generated deployment state before handoff"
  fi
done

claim_files=(
  README.md
  docs/architecture.md
  docs/implementation-status.md
  docs/crypto-validation.md
  docs/failure-evidence.md
  docs/routing-testbed.md
  docs/bundle-protocol.md
  docs/ai-context-channels.md
  docs/identity-anchors.md
  docs/contracts-polygon.md
  docs/deployment-local.md
  docs/deployment-k8s.md
  docs/deployment-terraform.md
  docs/operations-runbook.md
  docs/final-handoff.md
  docs/evidence-index.md
  docs/final-architecture-summary.md
  docs/release-notes.md
)

unsafe_pattern='FIPS certified|ACVTS validated|ACVTS certified|production-ready|production ready|production anonymous|production anonymity guarantee|audited smart contract|zero packet loss|censorship-resistant|live Polygon deployment|deployed across three continents'
if grep -E -i "$unsafe_pattern" "${claim_files[@]}" 2>/dev/null | grep -E -vi 'not |no |does not|do not|must not|without |only|non-claims|unsafe|doesnt|is not|are not' >/tmp/pq-fabric-hygiene-claims.txt; then
  cat /tmp/pq-fabric-hygiene-claims.txt >&2
  report_failure "unsafe claim language found"
fi

if [[ "$failures" -ne 0 ]]; then
  exit 1
fi

echo "repo-hygiene: pass"
