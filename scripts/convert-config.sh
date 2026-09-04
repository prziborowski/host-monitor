#!/usr/bin/env bash
#
# convert-config.sh
#
# Convert the host-monitor JSON config (config/hosts.json) into Kubernetes
# manifests so the configuration can be supplied to a Deployment via a
# ConfigMap (and optionally a Secret) instead of being baked into the image.
#
# It emits:
#   * a ConfigMap whose data key `hosts.json` holds the JSON as a literal
#     block scalar, and
#   * optionally a Secret holding the Slack API token (with --split-secret).
#
# The app reads its config path from the CONFIG_PATH env var (a Deployment
# sets CONFIG_PATH=/config/hosts.json and mounts the ConfigMap there).
#
# Usage:
#   convert-config.sh [options]
#
# Options:
#   --input PATH         Input JSON config file (default: config/hosts.json).
#   --configmap-name N   ConfigMap name      (default: host-monitor).
#   --secret-name N      Secret name         (default: host-monitor-secret).
#   --split-secret       Strip slack.api_key from the ConfigMap (set it to ""
#                        with a comment) and emit a separate Secret manifest
#                        holding the token under key `slack-api-key`.
#   --output-dir DIR     Write <configmap-name>.yaml and, with --split-secret,
#                        <secret-name>.yaml into DIR. Default: print to stdout.
#   -h, --help           Show this help and exit.
#
set -euo pipefail

INPUT="config/hosts.json"
CONFIGMAP_NAME="host-monitor"
SECRET_NAME="host-monitor-secret"
SPLIT_SECRET=0
OUTPUT_DIR=""

usage() {
  cat <<'USAGE'
Usage: convert-config.sh [options]

Convert a host-monitor JSON config into Kubernetes ConfigMap (and optional
Secret) manifests so the config can be supplied to a Deployment via a
ConfigMap instead of being embedded in the image.

Options:
   --input PATH         Input JSON config file (default: config/hosts.json).
   --configmap-name N   ConfigMap name      (default: host-monitor).
   --secret-name N      Secret name         (default: host-monitor-secret).
   --split-secret       Strip slack.api_key from the ConfigMap (set it to ""
                        with a comment) and emit a separate Secret manifest
                        holding the token under key `slack-api-key`.
   --output-dir DIR     Write <configmap-name>.yaml and, with --split-secret,
                        <secret-name>.yaml into DIR. Default: print to stdout.
   -h, --help           Show this help and exit.
USAGE
}

require_arg() {
  # require_arg VALUE FLAG_NAME
  if [[ -z "${1:-}" ]]; then
    echo "Error: option $2 requires a value." >&2
    exit 2
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    --input)
      require_arg "${2:-}" "$1"; INPUT="$2"; shift 2 ;;
    --input=*)
      INPUT="${1#*=}"; shift ;;
    --configmap-name)
      require_arg "${2:-}" "$1"; CONFIGMAP_NAME="$2"; shift 2 ;;
    --configmap-name=*)
      CONFIGMAP_NAME="${1#*=}"; shift ;;
    --secret-name)
      require_arg "${2:-}" "$1"; SECRET_NAME="$2"; shift 2 ;;
    --secret-name=*)
      SECRET_NAME="${1#*=}"; shift ;;
    --split-secret)
      SPLIT_SECRET=1; shift ;;
    --output-dir)
      require_arg "${2:-}" "$1"; OUTPUT_DIR="$2"; shift 2 ;;
    --output-dir=*)
      OUTPUT_DIR="${1#*=}"; shift ;;
    *)
      echo "Error: unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if ! command -v python3 >/dev/null 2>&1; then
  echo "Error: python3 is required but was not found in PATH." >&2
  exit 1
fi

if [[ ! -f "$INPUT" ]]; then
  echo "Error: input config not found: $INPUT" >&2
  exit 1
fi

python3 - "$INPUT" "$CONFIGMAP_NAME" "$SECRET_NAME" "$SPLIT_SECRET" "$OUTPUT_DIR" <<'PYEOF'
import json, sys, os, copy, datetime

input_path, configmap_name, secret_name = sys.argv[1], sys.argv[2], sys.argv[3]
split_secret = sys.argv[4] == "1"
output_dir = sys.argv[5]

with open(input_path) as f:
    config = json.load(f)

timestamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def header(note=None):
    lines = [
        "# Generated from " + input_path + " by convert-config.sh",
        "# Timestamp: " + timestamp,
        "# Do not edit by hand; regenerate with convert-config.sh.",
    ]
    if note:
        lines.append("# " + note)
    return lines


def block_scalar(obj, indent=4):
    # Render the JSON as a literal block scalar body (every line indented).
    text = json.dumps(obj, indent=2, ensure_ascii=False)
    pad = " " * indent
    return "\n".join((pad + line) if line else line for line in text.split("\n"))


def build_configmap(cfg, name, split):
    lines = list(header())
    lines.append("")
    lines.append("apiVersion: v1")
    lines.append("kind: ConfigMap")
    lines.append("metadata:")
    lines.append("  name: " + name)
    lines.append("  labels:")
    lines.append("    app: host-monitor")
    lines.append("data:")
    if split:
        lines.append('   # slack.api_key moved to Secret "' + secret_name + '" (key: slack-api-key)')
    lines.append("  hosts.json: |")
    lines.extend(block_scalar(cfg).split("\n"))
    return "\n".join(lines) + "\n"


def build_secret(name, token):
    lines = [
        "# Generated from " + input_path + " by convert-config.sh",
        "# Timestamp: " + timestamp,
        "# WARNING: this file contains a REAL SECRET (Slack API token).",
        "# Do NOT commit it. Inject it into the Deployment as a Secret.",
        "# The app reads its config path from the CONFIG_PATH env var",
        "# (a Deployment sets CONFIG_PATH=/config/hosts.json).",
        "",
        "apiVersion: v1",
        "kind: Secret",
        "metadata:",
        "  name: " + name,
        "  labels:",
        "    app: host-monitor",
        "type: Opaque",
        "stringData:",
        "  slack-api-key: " + json.dumps(token, ensure_ascii=False),
    ]
    return "\n".join(lines) + "\n"


# Config that goes into the ConfigMap.
cm_config = copy.deepcopy(config)
token = ""
if isinstance(config.get("slack"), dict) and "api_key" in config["slack"]:
    token = config["slack"].get("api_key") or ""

if split_secret:
    if isinstance(cm_config.get("slack"), dict):
        cm_config["slack"]["api_key"] = ""

cm_yaml = build_configmap(cm_config, configmap_name, split_secret)
secret_yaml = build_secret(secret_name, token) if split_secret else None

if output_dir:
    os.makedirs(output_dir, exist_ok=True)
    cm_path = os.path.join(output_dir, configmap_name + ".yaml")
    with open(cm_path, "w") as f:
        f.write(cm_yaml)
    sys.stderr.write("Wrote " + cm_path + "\n")
    if secret_yaml is not None:
        secret_path = os.path.join(output_dir, secret_name + ".yaml")
        with open(secret_path, "w") as f:
            f.write(secret_yaml)
        sys.stderr.write("Wrote " + secret_path + "\n")
else:
    sys.stdout.write(cm_yaml)
    if secret_yaml is not None:
        sys.stdout.write("---\n")
        sys.stdout.write(secret_yaml)
PYEOF
