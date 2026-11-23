# Deployment

This directory holds deployment assets for the single-binary server and optional sidecar services.

```text
deploy/
├── firecortex.service   Example systemd unit for the single-binary server
├── secrets/             Desktop signing secret template (unused unless building desktop manually)
├── services/            Optional document extraction / OCR services (Tika, Docling, Tesseract, RapidOCR)
└── windows/             Managed Windows desktop rollout docs and templates (desktop unsupported by default)
```

FireCortex is deployed as a single self-contained binary (`CGO_ENABLED=0`, PostgreSQL + pgvector). See the [Binary Deployment guide](../README.md#binary-deployment) in the root README for installation, configuration (`config.full.example.json`), systemd setup, and starting the optional services under `deploy/services/`.

> **Note:** Desktop builds and SQLite deployments are unsupported by default in this fork. Desktop packages are only built when `Desktop Release` is triggered manually via `workflow_dispatch`.
