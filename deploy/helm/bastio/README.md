# Bastio Helm chart

> ⚠️ **Alpha in v2.0.** Functional and CI-tested, but the
> `values.yaml` surface may shift in v2.1 based on early-adopter
> feedback. Pin `--version` when you upgrade.

Deploys the all-in-one Bastio binary (server + worker + ingest in
one process) to Kubernetes. PostgreSQL, Redis, and ClickHouse are
expected to live outside the chart.

## Prerequisites

- Kubernetes ≥ 1.24
- Helm ≥ 3.10
- A reachable PostgreSQL 18 (or 15+) instance
- A reachable Redis 7 instance
- (Optional) A reachable ClickHouse 24 instance for analytics

## Install

```bash
helm install bastio ./deploy/helm/bastio \
  --namespace bastio --create-namespace \
  --set secrets.databaseURL='postgres://bastio:bastio@pg:5432/bastio?sslmode=require' \
  --set secrets.redisURL='redis://redis:6379/0'
```

For a real environment, put the secrets in a values file (or a
managed secret pulled in via `secrets.externalSecret.name`) rather
than `--set` so they don't end up in shell history.

## Upgrade

```bash
helm upgrade bastio ./deploy/helm/bastio --namespace bastio --reuse-values
```

The chart's pod template hashes the configmap + secret content into
annotations, so `helm upgrade` always rolls the deployment when
config changes.

## Uninstall

```bash
helm uninstall bastio --namespace bastio
```

This removes the deployment, service, configmap, and (if
chart-managed) secret. It does NOT touch your PostgreSQL data.

## Values reference

See `values.yaml` — every key is commented inline. Highlights:

| Key | Default | Purpose |
|---|---|---|
| `image.repository` | `ghcr.io/bastio-ai/bastio` | Multi-arch image |
| `replicaCount` | `1` | Bump to 2–3 behind a load balancer for HA |
| `secrets.databaseURL` | `""` | Required |
| `secrets.redisURL` | `""` | Required |
| `secrets.clickhouseURL` | `""` | Optional — analytics off when empty |
| `secrets.externalSecret.name` | `""` | Use an existing K8s Secret instead |
| `ingress.enabled` | `false` | Off by default; bring your own |
| `config.mode` | `all-in-one` | Split-binary mode is on the v2.1 roadmap |
| `extraEnv` | `[]` | Pass cloud-overlay knobs (e.g. `BASTIO_WORKSPACE_URL`) |

## What the chart deploys

- A `Deployment` running the all-in-one binary on port 4000.
- A `Service` exposing port 80 → 4000.
- An `Ingress` (optional, off by default).
- A `ConfigMap` for non-secret env (mode, port, log level).
- A `Secret` for `DATABASE_URL`, `REDIS_URL`, optional
  `CLICKHOUSE_URL`, optional provider keys.
- A `ServiceAccount` (created unless `serviceAccount.create=false`).

## Probes

`/health` (liveness) checks Postgres + Redis + ClickHouse.
`/ready` (readiness) checks Postgres + Redis (ClickHouse is
allowed to be down — the gateway degrades gracefully). See
`pkg/server/server.go` for the implementations.

## What's NOT in this chart

By design, none of:

- PostgreSQL / Redis / ClickHouse subcharts. Use a managed service or
  the bitnami charts. Operators want explicit control over backups,
  HA topology, and version pinning.
- HPA / VPA. Add your own based on your actual traffic shape.
- Network policies. They're cluster-policy specific; add what your
  org requires.

These are likely v2.1 follow-ups once we have feedback from real
deployments.
