# Contributing

Changes should preserve the supported product scope in [docs/productization.md](docs/productization.md) and the operational guarantees in [docs/operations.md](docs/operations.md).

For code changes, run the relevant Go tests and checks. For user-facing changes, run the web build and browser checks documented in [docs/e2e-testing.md](docs/e2e-testing.md). Include the behavior changed, validation performed, and any migration or rollback impact in pull requests.

Do not include credentials, production data, database files, uploaded media, or generated release artifacts in a change. Dependency updates should include the lockfile changes and a summary of the affected packages.
