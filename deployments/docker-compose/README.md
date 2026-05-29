# Docker Compose

The canonical Phase 9 Docker Compose file is the repository-root `docker-compose.yml`.

Use:

```bash
make compose-config
make compose-up
make compose-down
make compose-clean
```

The root Compose file keeps validator services compatible with the original local flow while adding profiles for relays, demos, evidence, and anchors.
