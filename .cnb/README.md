# CNB test modules

The CNB pipelines run the root modules of the framework and all four drivers in
an explicit Go workspace. The integration pipeline adds `framework/tests` to
that workspace and runs its full suite, including the ported database-fixture
lifecycle cases. Driver cases run against the new runtime APIs. No old GORM
implementation or compatibility module is retained.

The current repository always uses the checkout supplied by CNB. For each other
repository, configure these CNB pipeline environment variables:

| Repository | Clone URL | Compatible branch, tag, or commit |
| --- | --- | --- |
| framework | `GRADII_FRAMEWORK_URL` | `GRADII_FRAMEWORK_REF` |
| mysql | `GRADII_MYSQL_URL` | `GRADII_MYSQL_REF` |
| postgres | `GRADII_POSTGRES_URL` | `GRADII_POSTGRES_REF` |
| sqlite | `GRADII_SQLITE_URL` | `GRADII_SQLITE_REF` |
| sqlserver | `GRADII_SQLSERVER_URL` | `GRADII_SQLSERVER_REF` |

The framework URL defaults to its existing remote,
`https://cnb.cool/gradii/framework`. Driver URLs and all companion refs must be
configured explicitly. The candidate `cnb.cool/gradii/{driver}` repositories
were unavailable when these pipelines were added, and the original
`goravel/{driver}` repositories do not contain the new module namespaces. Use
refs that contain the coordinated migration; pin commit IDs for reproducible
runs. Each checkout is validated against its expected `github.com/gradii/...`
module name before testing. Authentication uses the runner's Git configuration;
URLs should not contain credentials.

`bash .cnb/ci.sh unit` tests all five root modules.
`bash .cnb/ci.sh database` tests the framework database packages.
`bash .cnb/ci.sh integration` tests `framework/tests`.
Upstream driver cases are ported to the new runtime tests; fixture lifecycle
cases run in `framework/tests`. No archived GORM implementation is tested.

The integration suite uses the existing CNB `docker` service on the `cnb:arch:amd64` runner. Their tests start
their own database containers and connect through the shared daemon's published
loopback ports. The script checks Docker availability and fails if the service
is unavailable. SQLite tests use local database files. Every suite
executes normally, with `-count=1`; none is replaced by a compilation-only check.

The helper creates an isolated temporary workspace and removes it on exit. It
does not depend on a developer's ignored `go.work`, rewrite module declarations,
or run `go mod tidy`. Each repository carries the same `ci.sh` so its CNB
checkout can prepare the workspace without fetching an unpinned helper script.

## GitHub mirror

Every repository of this workspace mirrors itself to its GitHub home,
`github.com/gradii/<module>`, from the CNB pipeline. Only two things are
published, and only from `main`: a push to `main` publishes that branch
together with every `v*` tag the CNB repository holds, and a `v*` tag push
publishes that tag. No other branch is mirrored.

The destination is derived from the module path in `go.mod` and the credential
from `DEPLOY_GITHUB_GRADII_<MODULE>_TOKEN` (`FRAMEWORK`, `MYSQL`, `POSTGRES`,
`SQLITE`, `SQLSERVER`). The shared secret file
`https://cnb.cool/gradii/secret/-/blob/main/deploy-github/gradii.yml` provides
all five, and the mirror jobs import it. When a token is absent the mirror
skips the run with a message rather than failing, so a repository whose token is
not configured yet still builds.

`bash .cnb/mirror-github.sh branch` publishes `main` and its version tags,
`bash .cnb/mirror-github.sh tag` publishes the tag named by `CNB_BRANCH`. The
script is identical in all five repositories, like `ci.sh`.

The Gradii module line starts at `v1.20.0`. That is the first tag these modules
publish, and the mirror skips everything below it: the older tags are upstream
release history and point at commits that predate the native-contract ports. A
mirrored tag keeps its original target commit, so a tag that already exists on
GitHub is republished unchanged.
