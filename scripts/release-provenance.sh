#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p tmp

json="tmp/release-provenance.json"
text="tmp/release-provenance.txt"
modules="tmp/go-modules.txt"
sbom_file="${PQFABRIC_SBOM_OUT:-tmp/sbom.spdx.json}"
image_digest_file="${PQFABRIC_IMAGE_DIGEST_OUT:-tmp/image-digest.txt}"
cosign_verify_file="${PQFABRIC_COSIGN_VERIFY_OUT:-tmp/cosign-verify.txt}"
image_ref="${PQFABRIC_IMAGE_REF:-pq-fabric:local}"
release_mode="${PQFABRIC_RELEASE_MODE:-local}"
cosign_verify="${PQFABRIC_COSIGN_VERIFY:-false}"
cosign_identity_regexp="${PQFABRIC_COSIGN_CERT_IDENTITY_REGEXP:-https://github.com/keithwegner/pq-fabric/.github/workflows/release-artifacts.yml@refs/(heads/main|tags/v.*)}"
cosign_oidc_issuer="${PQFABRIC_COSIGN_CERT_OIDC_ISSUER:-https://token.actions.githubusercontent.com}"
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

if [[ "$release_mode" != "local" && "$release_mode" != "published" ]]; then
  echo "release-provenance: PQFABRIC_RELEASE_MODE must be local or published" >&2
  exit 2
fi

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
if [[ "$image_ref" == *@sha256:* ]]; then
  image_digest="$image_ref"
  image_id="registry digest reference"
elif command -v docker >/dev/null 2>&1 && docker image inspect "$image_ref" >/tmp/pq-fabric-image-inspect.json 2>/dev/null; then
  image_id="$(docker image inspect "$image_ref" --format '{{.Id}}' 2>/dev/null || true)"
  image_digest="$(docker image inspect "$image_ref" --format '{{index .RepoDigests 0}}' 2>/dev/null || true)"
  if [[ -z "$image_digest" || "$image_digest" == "<no value>" ]]; then
    image_digest="image present without repo digest"
  fi
fi
printf '%s\n' "$image_digest" >"$image_digest_file"

sbom_status="skipped: syft not installed"
if [[ -s "$sbom_file" ]]; then
  sbom_status="pass: ${sbom_file}"
elif command -v syft >/dev/null 2>&1; then
  if syft packages dir:. -o spdx-json="$sbom_file" >/tmp/pq-fabric-syft.out 2>&1; then
    sbom_status="pass: ${sbom_file}"
  else
    sbom_status="failed: $(head -c 300 /tmp/pq-fabric-syft.out | tr '\n' ' ')"
  fi
fi

cosign_status="skipped: cosign not installed"
printf '%s\n' "$cosign_status" >"$cosign_verify_file"
if command -v cosign >/dev/null 2>&1; then
  verify_ref="$image_ref"
  if [[ "$verify_ref" != *@sha256:* && "$image_digest" == *@sha256:* ]]; then
    verify_ref="$image_digest"
  fi
  if [[ "$cosign_verify" == "true" && "$verify_ref" == *@sha256:* ]]; then
    if cosign verify \
      --certificate-identity-regexp "$cosign_identity_regexp" \
      --certificate-oidc-issuer "$cosign_oidc_issuer" \
      "$verify_ref" >"$cosign_verify_file" 2>&1; then
      cosign_status="pass: verified ${verify_ref}"
    else
      cosign_status="failed: $(head -c 300 "$cosign_verify_file" | tr '\n' ' ')"
    fi
  else
    cosign_status="skipped: set PQFABRIC_COSIGN_VERIFY=true and PQFABRIC_IMAGE_REF to a digest-pinned signed image"
    printf '%s\n' "$cosign_status" >"$cosign_verify_file"
  fi
fi

published_requirements="not_applicable"
status="pass"
if [[ "$go_version" == failed:* || "$sbom_status" == failed:* || "$cosign_status" == failed:* ]]; then
  status="fail"
fi

if [[ "$release_mode" == "published" ]]; then
  missing=()
  [[ "$git_dirty" == "false" ]] || missing+=("clean git tree")
  [[ "$image_digest" == *@sha256:* ]] || missing+=("digest-pinned image")
  [[ "$sbom_status" == pass:* ]] || missing+=("SBOM")
  [[ "$cosign_status" == pass:* ]] || missing+=("cosign verification")
  if (( ${#missing[@]} > 0 )); then
    published_requirements="failed: ${missing[*]}"
    status="fail"
  else
    published_requirements="pass"
  fi
elif [[ "$status" == "pass" && ( "$image_digest" == skipped:* || "$image_digest" == "image present without repo digest" || "$sbom_status" == skipped:* || "$cosign_status" == skipped:* ) ]]; then
  status="pass_with_skips"
fi

cat >"$json" <<JSON
{
  "schema_version": "pq-fabric.release-provenance.v1",
  "generated_at_unix_milli": ${generated_at},
  "status": "$(json_escape "$status")",
  "release_mode": "$(json_escape "$release_mode")",
  "published_requirements": "$(json_escape "$published_requirements")",
  "git_ref": "$(json_escape "$git_ref")",
  "git_dirty": "$(json_escape "$git_dirty")",
  "go_version": "$(json_escape "$go_version")",
  "go_module_count": ${module_count},
  "go_module_inventory": "$(json_escape "$modules")",
  "image_reference": "$(json_escape "$image_ref")",
  "docker_image_id": "$(json_escape "$image_id")",
  "docker_image_digest": "$(json_escape "$image_digest")",
  "image_digest_file": "$(json_escape "$image_digest_file")",
  "sbom_file": "$(json_escape "$sbom_file")",
  "sbom_status": "$(json_escape "$sbom_status")",
  "cosign_verify_requested": "$(json_escape "$cosign_verify")",
  "cosign_status": "$(json_escape "$cosign_status")",
  "cosign_verify_file": "$(json_escape "$cosign_verify_file")",
  "limitations": "Release provenance evidence only; no cloud deployment, Terraform apply, Kubernetes apply, secret fetch, Polygon mainnet, or certification claim is performed. Registry publish and image signing are performed only by the release-artifacts workflow on main or v* tags."
}
JSON

{
  echo "pq-fabric release provenance"
  echo "generated_at_unix_milli: ${generated_at}"
  echo "status: ${status}"
  echo "release_mode: ${release_mode}"
  echo "published_requirements: ${published_requirements}"
  echo "git_ref: ${git_ref}"
  echo "git_dirty: ${git_dirty}"
  echo "go_version: ${go_version}"
  echo "go_module_count: ${module_count}"
  echo "go_module_inventory: ${modules}"
  echo "image_reference: ${image_ref}"
  echo "docker_image_id: ${image_id}"
  echo "docker_image_digest: ${image_digest}"
  echo "image_digest_file: ${image_digest_file}"
  echo "sbom_file: ${sbom_file}"
  echo "sbom_status: ${sbom_status}"
  echo "cosign_verify_requested: ${cosign_verify}"
  echo "cosign_status: ${cosign_status}"
  echo "cosign_verify_file: ${cosign_verify_file}"
  echo "limitations: Evidence only; no cloud deployment, Terraform apply, Kubernetes apply, secret fetch, Polygon mainnet, or certification claim. Registry publish and image signing are performed only by the release-artifacts workflow on main or v* tags."
} >"$text"

cat "$text"
