#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p tmp

json="tmp/release-provenance.json"
text="tmp/release-provenance.txt"
modules="tmp/go-modules.txt"
sbom_file="${PQFABRIC_SBOM_OUT:-tmp/sbom.spdx.json}"
image_ref="${PQFABRIC_IMAGE_REF:-pq-fabric:local}"
cosign_verify="${PQFABRIC_COSIGN_VERIFY:-false}"
generated_at="$(date +%s000)"

json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

tool_status() {
  local name="$1"
  shift
  if ! command -v "$name" >/dev/null 2>&1; then
    printf 'skipped: %s not installed' "$name"
    return
  fi
  if "$@" >/tmp/pq-fabric-release-tool.out 2>&1; then
    head -c 400 /tmp/pq-fabric-release-tool.out | tr '\n' ' '
  else
    printf 'failed: '
    head -c 300 /tmp/pq-fabric-release-tool.out | tr '\n' ' '
  fi
}

go_version="$(tool_status go go version)"
module_count="0"
if command -v go >/dev/null 2>&1; then
  if go list -m all >"$modules" 2>/tmp/pq-fabric-go-modules.err; then
    module_count="$(wc -l <"$modules" | tr -d ' ')"
  else
    module_count="0"
    rm -f "$modules"
  fi
fi

git_ref="unavailable: no git metadata"
git_dirty="unknown"
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git_ref="$(git rev-parse HEAD)"
  if [[ -n "$(git status --short)" ]]; then
    git_dirty="true"
  else
    git_dirty="false"
  fi
fi

image_id="skipped: docker image ${image_ref} not available"
image_digest="skipped: docker image ${image_ref} not available"
if command -v docker >/dev/null 2>&1 && docker image inspect "$image_ref" >/tmp/pq-fabric-image-inspect.json 2>/dev/null; then
  image_id="$(docker image inspect "$image_ref" --format '{{.Id}}' 2>/dev/null || true)"
  image_digest="$(docker image inspect "$image_ref" --format '{{index .RepoDigests 0}}' 2>/dev/null || true)"
  if [[ -z "$image_digest" || "$image_digest" == "<no value>" ]]; then
    image_digest="image present without repo digest"
  fi
fi

sbom_status="skipped: syft not installed"
if command -v syft >/dev/null 2>&1; then
  if syft packages dir:. -o spdx-json="$sbom_file" >/tmp/pq-fabric-syft.out 2>&1; then
    sbom_status="pass: ${sbom_file}"
  else
    sbom_status="failed: $(head -c 300 /tmp/pq-fabric-syft.out | tr '\n' ' ')"
  fi
fi

cosign_status="skipped: cosign not installed"
if command -v cosign >/dev/null 2>&1; then
  if [[ "$cosign_verify" == "true" && -n "${PQFABRIC_IMAGE_REF:-}" ]]; then
    if cosign verify "$image_ref" >/tmp/pq-fabric-cosign.out 2>&1; then
      cosign_status="pass: verified ${image_ref}"
    else
      cosign_status="failed: $(head -c 300 /tmp/pq-fabric-cosign.out | tr '\n' ' ')"
    fi
  else
    cosign_status="skipped: set PQFABRIC_COSIGN_VERIFY=true and PQFABRIC_IMAGE_REF to verify a signed image"
  fi
fi

status="pass"
if [[ "$go_version" == failed:* || "$sbom_status" == failed:* || "$cosign_status" == failed:* ]]; then
  status="fail"
elif [[ "$image_digest" == skipped:* || "$sbom_status" == skipped:* || "$cosign_status" == skipped:* ]]; then
  status="pass_with_skips"
fi

cat >"$json" <<JSON
{
  "schema_version": "pq-fabric.release-provenance.v1",
  "generated_at_unix_milli": ${generated_at},
  "status": "$(json_escape "$status")",
  "git_ref": "$(json_escape "$git_ref")",
  "git_dirty": "$(json_escape "$git_dirty")",
  "go_version": "$(json_escape "$go_version")",
  "go_module_count": ${module_count},
  "go_module_inventory": "$(json_escape "$modules")",
  "image_reference": "$(json_escape "$image_ref")",
  "docker_image_id": "$(json_escape "$image_id")",
  "docker_image_digest": "$(json_escape "$image_digest")",
  "sbom_file": "$(json_escape "$sbom_file")",
  "sbom_status": "$(json_escape "$sbom_status")",
  "cosign_verify_requested": "$(json_escape "$cosign_verify")",
  "cosign_status": "$(json_escape "$cosign_status")",
  "limitations": "Release provenance evidence only; no registry publish, cloud deployment, Terraform apply, secret fetch, or certification claim is performed. Image signing is verified only when explicitly configured."
}
JSON

{
  echo "pq-fabric release provenance dry-run"
  echo "generated_at_unix_milli: ${generated_at}"
  echo "status: ${status}"
  echo "git_ref: ${git_ref}"
  echo "git_dirty: ${git_dirty}"
  echo "go_version: ${go_version}"
  echo "go_module_count: ${module_count}"
  echo "go_module_inventory: ${modules}"
  echo "image_reference: ${image_ref}"
  echo "docker_image_id: ${image_id}"
  echo "docker_image_digest: ${image_digest}"
  echo "sbom_file: ${sbom_file}"
  echo "sbom_status: ${sbom_status}"
  echo "cosign_verify_requested: ${cosign_verify}"
  echo "cosign_status: ${cosign_status}"
  echo "limitations: Evidence only; no registry publish, cloud deployment, Terraform apply, secret fetch, or certification claim. Image signing is verified only when explicitly configured."
} >"$text"

cat "$text"
