## Lesson 4: Tooling & Environment Discrepancies on Host Platforms

- **Incident & Tracking:** Local Colima & Docker Compose onboarding (`make dev-model`).
- **Symptom:**
  Executing `make dev-model` (`docker compose -f docker-compose.local.yml up -d`) failed on developer macOS machines with:
  ```
  unknown shorthand flag: 'f' in -f
  Usage: docker [OPTIONS] COMMAND [ARG...]
  make: *** [dev-model] Error 125
  ```
- **Root Cause:**
  On macOS systems using Homebrew without Docker Desktop, `docker-compose` is installed as a standalone binary in `/opt/homebrew/bin/docker-compose`, rather than as a Docker CLI plugin in `~/.docker/cli-plugins/docker-compose`. As a result, the Docker CLI did not recognize the subcommand `compose` (with space) and attempted to parse `-f` as a root flag.
- **Mandatory Takeaway:**
  1. **Multi-Environment Resilience**: Tooling scripts and Makefiles should support both standalone binaries and CLI plugins, or document exact plugin symlinking:
     ```bash
     mkdir -p ~/.docker/cli-plugins && ln -sfn $(which docker-compose) ~/.docker/cli-plugins/docker-compose
     ```
