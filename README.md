# Grunter

GitOps IaC pipeline for GitLab — like [Gruntwork Pipelines](https://www.gruntwork.io/pipelines), but for GitLab CI.

Grunter detects which Terragrunt units changed in a Merge Request, resolves their dependency order, runs `terragrunt plan`, and posts the results as formatted MR comments. On merge, it runs `terragrunt apply` in the correct order.

## How it works

```
GitLab MR opened → CI triggers grunter → detects changed units →
resolves dependency graph → runs terragrunt plan → posts MR comments
```

```
MR merged → CI triggers grunter → detects changed units →
resolves dependency graph → runs terragrunt apply in order
```

## Install

```bash
go install github.com/justinholmes/grunter@latest
```

Or build from source:

```bash
git clone git@github.com:justinholmes/grunter.git
cd grunter
go build -o grunter .
```

## Quick start

1. Add a `.grunter/config.yml` to your infra repo:

```yaml
deploy_branch: main
tf_binary: opentofu       # or "terraform"
tg_binary: terragrunt
ignore:
  - "**/.terraform.lock.hcl"
  - "**/README.md"
```

2. Copy `templates/gitlab-ci.yml` into your repo as `.gitlab-ci.yml` (or include it).

3. Open a Merge Request that modifies a Terragrunt unit. Grunter will detect the changes, run plans, and post the results as MR comments.

## Commands

### `grunter orchestrate`

Detect changed Terragrunt units and output a JSON execution plan.

```bash
grunter orchestrate --base <sha> --head <sha> [-o plan.json]
```

| Flag | Default | Env var fallback |
|------|---------|-----------------|
| `--base` | — | `CI_MERGE_REQUEST_DIFF_BASE_SHA` |
| `--head` | — | `CI_COMMIT_SHA` |
| `-o, --output` | stdout | — |

Output:

```json
{
  "layers": [
    { "units": [{ "path": "networking/vpc", "change_type": "ModuleChanged" }] },
    { "units": [{ "path": "data/rds", "change_type": "ModuleChanged" },
                { "path": "services/api", "change_type": "ModuleChanged" }] }
  ]
}
```

Units in the same layer have no inter-dependencies and can run in parallel.

### `grunter execute`

Run `terragrunt plan` or `terragrunt apply` on a single unit.

```bash
grunter execute plan <unit-path> [-o output.txt] [--timeout 30m]
grunter execute apply <unit-path> [-o output.txt] [--timeout 30m]
```

Uses `-detailed-exitcode` for plan (exit 2 = changes detected, not an error) and `-auto-approve` for apply.

### `grunter comment`

Post plan/apply results to a GitLab MR as a formatted markdown comment.

```bash
grunter comment --unit <path> --action plan --input output.txt \
  --project-id 123 --mr-iid 1 --gitlab-url https://gitlab.example.com
```

| Flag | Default | Env var fallback |
|------|---------|-----------------|
| `--input` | stdin | — |
| `--unit` | — | — |
| `--action` | `plan` | — |
| `--project-id` | — | `CI_PROJECT_ID` |
| `--mr-iid` | — | `CI_MERGE_REQUEST_IID` |
| `--gitlab-url` | `https://gitlab.com` | `CI_SERVER_URL` |

Requires `GITLAB_TOKEN` or `CI_JOB_TOKEN` environment variable.

Comments are idempotent — re-running updates the existing comment instead of creating a duplicate.

### `grunter drift`

Scan all Terragrunt units and run `plan` on each to detect drift.

```bash
grunter drift [--root .] [-o drift-report.md]
```

Outputs a markdown drift report listing units where the plan shows changes.

## Configuration

Default config path: `.grunter/config.yml` (override with `--config`).

```yaml
deploy_branch: main        # Branch that triggers apply (default: main)
tf_binary: opentofu        # "opentofu" or "terraform" (default: opentofu)
tg_binary: terragrunt      # Terragrunt binary (default: terragrunt)
ignore:                     # Glob patterns to exclude from change detection
  - "**/.terraform.lock.hcl"
  - "**/README.md"
```

## GitLab CI template

A reference `.gitlab-ci.yml` is provided in `templates/gitlab-ci.yml`. It defines three stages:

| Stage | Trigger | What it does |
|-------|---------|-------------|
| `detect` | MR or push to main | Runs `grunter orchestrate`, outputs execution plan |
| `plan` | MR only | Runs `grunter execute plan` + `grunter comment` per unit |
| `apply` | Push to main only | Runs `grunter execute apply` per unit in dependency order |

## Change detection

Grunter classifies changes from `git diff` into:

- **ModuleChanged** — a file changed inside a directory with `terragrunt.hcl`
- **ModuleAdded** — a new directory with `terragrunt.hcl`
- **ModuleDeleted** — a removed directory that had `terragrunt.hcl`
- **EnvCommonChanged** — changes in `_envcommon/` propagated to dependents

## Dependency resolution

Grunter parses `dependency` and `dependencies` blocks from `terragrunt.hcl` files using the HCL parser, builds a DAG, and groups units into execution layers via topological sort (Kahn's algorithm). Units within a layer can run in parallel.

## MR comment format

Plan comments include:
- Status icon (warning for destroys, checkmark for no changes)
- Resource change summary (`+N to add · -N to destroy`)
- Collapsible plan output with HCL syntax highlighting
- ANSI codes and terragrunt init noise stripped automatically

## Running tests

Unit tests:

```bash
go test ./...
```

Integration tests (require `tofu`, `terragrunt`, and `git` on PATH):

```bash
go test -tags=integration -timeout 10m ./test/
```

GitLab integration tests (require a running GitLab instance):

```bash
export GRUNTER_TEST_GITLAB_URL=http://localhost:8929
export GRUNTER_TEST_GITLAB_TOKEN=glpat-...
go test -tags=integration -timeout 10m ./test/ -run TestE2E_GitLabDrift
go test -tags=integration -timeout 10m ./internal/gitlab/
```

## License

MIT
