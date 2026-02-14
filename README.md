# uploader

## Local development

1. Create a `GHCR_TOKEN` secret (read-only or package write) in your shell.
2. Run `GHCR_TOKEN=… ./start.sh` to log into GHCR, spin up PostgreSQL, and start the bot. The script launches both `docker-compose.postgres.yml` and `docker-compose.yml` so that Postgres is bootstrapped automatically.
3. The bot reads `config.yaml`; if you need to override the DSN, either edit that file or set the `POSTGRES_DSN` environment variable before running the script.

The Postgres container shares the external network `uploader-net`, so other apps can connect to it by attaching to that same network and using the `postgres` host/built DSN.

You can also run `docker compose -f docker-compose.postgres.yml -f docker-compose.yml up` yourself if you prefer to manage each command manually.
 