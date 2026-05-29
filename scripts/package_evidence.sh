#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p dist/evidence-package
rm -rf dist/evidence-package
mkdir -p dist/evidence-package/evidence dist/evidence-package/docs

for file in tmp/*-evidence.json tmp/*-evidence.txt; do
  [[ -e "$file" ]] || continue
  cp "$file" dist/evidence-package/evidence/
done

cp docs/evidence-index.md docs/final-handoff.md docs/claim-safety-review.md docs/release-notes.md dist/evidence-package/docs/

artifact="dist/pq-fabric-evidence.tar.gz"
rm -f "$artifact"
tar -czf "$artifact" -C dist evidence-package

count="$(find dist/evidence-package -type f | wc -l | tr -d ' ')"
echo "created $artifact with $count files"
find dist/evidence-package -type f | sort
