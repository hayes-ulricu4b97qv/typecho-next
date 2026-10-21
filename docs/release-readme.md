# Typecho Next

Typecho Next is a self-hosted personal blog for Windows x64.

## Start

1. Extract this ZIP to a stable application directory.
2. Run `start.cmd` and keep its console open while the site is running.
3. Open <http://127.0.0.1:8080/setting> and create the owner account.

The launcher stores the SQLite database and uploaded images under `%LOCALAPPDATA%\TypechoNext\data` by default. Set `APP_DATA_DIR` or `APP_MEDIA_DIR` in the process environment before starting when you need different locations. The application does not load `.env` files.

The service listens on `127.0.0.1` and provides plain HTTP. For public access, place a reverse proxy in front of it, terminate HTTPS there, and set `APP_COOKIE_SECURE=true` for the application process. Do not expose the application port directly to the internet.

## Data and updates

Download a full backup from the owner dashboard before upgrading. Stop the application before replacing its files, keep the data directory, then start the new version and check `/readyz`, sign-in, articles, comments, and images. Database changes only support forward upgrades. To roll back, restore both the prior application and the database and media from the pre-upgrade backup; do not run an older binary against a migrated database.

See [docs/operations.md](docs/operations.md) for configuration, backups, recovery, and detailed upgrade steps. `VERSION` identifies this package; `BUILD.txt` records its source commit. The matching source code is available from the repository tag archive.
