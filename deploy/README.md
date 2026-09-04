# Deploying host-monitor to Kubernetes

`host-monitor` is a long-running daemon that pings a list of hosts and posts
consolidated alerts to Slack. It opens **no HTTP port**, so no `Service` or
`Ingress` is required, and it defines **no readiness/liveness probe** (there is
no endpoint to check).

The manifests in this directory are **plain, standalone Kubernetes YAML**. You
can apply them with plain `kubectl apply -f` — **no `kustomize` binary
required.** The `kustomization.yaml` is only an optional convenience for those
who do have kustomize.

**Namespace:** all resources are deployed into a dedicated `host-monitor`
namespace so they stay organized together instead of landing in `default`. The
namespace is created automatically for you — by `deploy/namespace.yaml`, by
`apply.sh` (which applies it first), and by `kustomization.yaml` (via its
top-level `namespace:`).

Files in this directory:

| File              | Purpose                                                         |
| ---------------- | -------------------------------------------------------------- |
| `namespace.yaml`| The dedicated `host-monitor` namespace all resources land in.    |
| `configmap.yaml` | Config (hosts, Slack, ping, status_update) as `hosts.json`.     |
| `secret.yaml`     | **Optional** hardening: Slack token kept out of the ConfigMap. |
| `deployment.yaml` | The Deployment (1 replica, non-root, `NET_RAW`).                |
| `apply.sh`       | No-kustomize helper that runs `kubectl apply -f` for you.       |
| `kustomization.yaml` | **Optional** kustomize bundle for `kubectl apply -k`.          |

---

## 1. Build & push the image

The manifests reference the image `host-monitor:latest`. Build and tag it:

```bash
# from the repository root
docker build -t host-monitor:latest .

# push it to a registry so your cluster can pull it:
docker tag host-monitor:latest myregistry.example.com/host-monitor:latest
docker push myregistry.example.com/host-monitor:latest
```

Then point the Deployment at your registry. **Without kustomize**, just edit the
`image:` field in `deploy/deployment.yaml` directly:

```yaml
image: myregistry.example.com/host-monitor:latest
```

> **With kustomize (optional):** if you have the kustomize binary, you can
> instead override the tag/registry with an `images:` transform in
> `kustomization.yaml` (a commented example is included there) without touching
> `deployment.yaml`:
>
> ```yaml
> images:
>     - name: host-monitor
>      newName: myregistry.example.com/host-monitor
>      newTag: latest
> ```

## 2. Generate the config

Create a real `hosts.json` from your local `config/hosts.json`:

```bash
# produce a ConfigMap (token stays in the ConfigMap)
scripts/convert-config.sh

# OPTIONAL hardening: move the Slack token into the Secret instead
scripts/convert-config.sh --split-secret
```

Then edit the placeholder values (`xoxb-REPLACE-ME`, IPs, channel, ...) in
`configmap.yaml` and, if you used `--split-secret`, in `secret.yaml`.

## 3. Apply

**Default path — no kustomize, just `kubectl apply -f`:**

Each manifest now carries `namespace: host-monitor` in its metadata, so create
the namespace first and `kubectl apply -f` lands the rest in the right place:

```bash
kubectl apply -f deploy/namespace.yaml
kubectl apply -f deploy/configmap.yaml -f deploy/deployment.yaml

# plus this ONLY if you are using the optional Secret:
kubectl apply -f deploy/secret.yaml
```

**Convenience helper — equivalent, no kustomize:**

```bash
# ConfigMap + Deployment:
bash deploy/apply.sh

# plus the optional Secret:
bash deploy/apply.sh --with-secret
```

`deploy/apply.sh` is a small `kubectl apply -f` wrapper that resolves the
manifest paths relative to its own location (so it works from any directory),
checks that `kubectl` is in `PATH`, and applies `secret.yaml` only when you pass
`--with-secret`. It is the no-kustomize alternative to `kubectl apply -k
deploy/`.

If you enabled the optional Secret, also **uncomment** the `SLACK_API_KEY`
env block in `deployment.yaml` so the token is injected from the Secret and left
blank in the ConfigMap.

> **With kustomize (optional):** if you do have the kustomize binary, the single
> command `kubectl apply -k deploy/` is equivalent (the Secret stays commented out
> in `kustomization.yaml` by default). **`kustomization.yaml` is optional** — if
> you don't have kustomize you can ignore it or delete it entirely; the no-kustomize
> path above needs nothing from it.

## 4. Notes

- **NET_RAW capability:** the container needs the `NET_RAW` Linux capability
  because `ping` uses an ICMP raw socket. This is added in
  `deployment.yaml` under `securityContext.capabilities.add`.
- **ICMP / network policy caveat:** some CNI plugins and Kubernetes
  NetworkPolicies block ICMP. If pings silently fail, check that ICMP egress is
  allowed from the pod to the target hosts.
- **No port / no Service:** the app has no HTTP endpoint, so nothing is exposed.
  There is also no probe because there is nothing to probe.
- **Viewing logs:**

  ```bash
  kubectl logs -f deploy/host-monitor -n host-monitor
  # or by label:
  kubectl logs -f -l app=host-monitor -n host-monitor
  ```

- **Updating config:** edit `configmap.yaml` (and re-run
  `scripts/convert-config.sh` if you prefer), then re-apply with
  `kubectl apply -f deploy/configmap.yaml -f deploy/deployment.yaml`
  (`bash deploy/apply.sh`, or `kubectl apply -k deploy/` if you have kustomize).
  The daemon reads `CONFIG_PATH=/config/hosts.json` on startup.

## 5. Security

The Slack bot token (`slack.api_key`) is a **sensitive secret**. The simplest
mode keeps the whole config (including the token) in the ConfigMap, which is fine
for local/dev use. **In production, prefer the Secret path**: use
`scripts/convert-config.sh --split-secret`, apply the Secret (e.g.
`kubectl apply -f deploy/secret.yaml` or `bash deploy/apply.sh --with-secret`),
and uncomment the `SLACK_API_KEY` env block in `deployment.yaml` so the token
never lives in the ConfigMap.
