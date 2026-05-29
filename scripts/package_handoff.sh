#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p dist
staging="dist/pq-fabric-handoff"
artifact="dist/pq-fabric-handoff.tar.gz"
rm -rf "$staging" "$artifact"
mkdir -p "$staging"

tar -cf - \
  --exclude='./.git' \
  --exclude='./data' \
  --exclude='./tmp' \
  --exclude='./dist' \
  --exclude='./.DS_Store' \
  --exclude='./**/.DS_Store' \
  --exclude='./.env' \
  --exclude='./.env.*' \
  --exclude='./*.pem' \
  --exclude='./*.key' \
  --exclude='./*.kubeconfig' \
  --exclude='./*wallet*' \
  --exclude='./node_modules' \
  --exclude='./vendor' \
  --exclude='./contracts/polygon/out' \
  --exclude='./contracts/polygon/cache' \
  --exclude='./contracts/polygon/broadcast' \
  --exclude='./deployments/terraform/.terraform' \
  --exclude='./deployments/terraform/terraform.tfstate' \
  --exclude='./deployments/terraform/terraform.tfstate.*' \
  --exclude='./deployments/terraform/**/*.tfvars' \
  --exclude='./deployments/terraform/**/*.auto.tfvars' \
  . | tar -xf - -C "$staging"

find "$staging" -name '.DS_Store' -delete
find "$staging" -name 'api-keys*.json' ! -name 'api-keys.example.json' -delete

mkdir -p "$staging/evidence"
for file in tmp/*-evidence.json tmp/*-evidence.txt; do
  [[ -e "$file" ]] || continue
  cp "$file" "$staging/evidence/"
done

tar -czf "$artifact" -C dist pq-fabric-handoff

source_count="$(find "$staging" -type f | wc -l | tr -d ' ')"
evidence_count="$(find "$staging/evidence" -type f 2>/dev/null | wc -l | tr -d ' ')"
echo "created $artifact"
echo "files_packaged=$source_count evidence_files=$evidence_count"
echo "sample contents:"
find "$staging" -maxdepth 2 -type f | sort | sed -n '1,80p'
