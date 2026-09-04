#!/usr/bin/env bash
#
# apply.sh — no-kustomize alternative to `kubectl apply -k deploy/`.
#
# The manifests in this directory are plain, standalone Kubernetes YAML that
# apply fine with `kubectl apply -f`. This script just wires up the right files
# (a dedicated namespace, then ConfigMap + Deployment, plus the optional Secret)
# so you do not need the kustomize binary at all.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

NAMESPACE="${SCRIPT_DIR}/namespace.yaml"
CONFIGMAP="${SCRIPT_DIR}/configmap.yaml"
DEPLOYMENT="${SCRIPT_DIR}/deployment.yaml"
SECRET="${SCRIPT_DIR}/secret.yaml"

usage() {
    cat <<'USAGE'
Usage: apply.sh [--with-secret] [--help|-h]

Apply the host-monitor manifests without kustomize.

Creates the host-monitor namespace first, then by default applies:
    namespace.yaml  configmap.yaml  deployment.yaml

Options:
     --with-secret   Also apply secret.yaml (optional Slack-token hardening).
     -h, --help      Show this help and exit.
USAGE
}

for arg in "$@"; do
    case "$arg" in
         --with-secret)
             WITH_SECRET=1
              ;;
         -h|--help)
             usage
             exit 0
              ;;
         *)
             echo "Unknown argument: $arg" >&2
             usage >&2
             exit 2
              ;;
    esac
done

if ! command -v kubectl >/dev/null 2>&1; then
    echo "error: kubectl not found in PATH. Install kubectl and add it to PATH." >&2
    exit 1
fi

# Create the namespace first: the other resources target it, so it must exist
# before they are applied.
echo "Applying namespace: ${NAMESPACE}"
kubectl apply -f "${NAMESPACE}"

FILES=("$CONFIGMAP" "$DEPLOYMENT")
if [ "${WITH_SECRET:-0}" = "1" ]; then
    FILES+=("$SECRET")
fi

ARGS=()
for f in "${FILES[@]}"; do
    ARGS+=(-f "$f")
done

echo "Applying: ${FILES[*]}"
kubectl apply "${ARGS[@]}"
