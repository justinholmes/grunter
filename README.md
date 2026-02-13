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

### Progressive deployments (optional)

With environments configured, Grunter supports canary-style progressive deployments:

```
MR merged → promote generates staged plan →
deploy to staging → canary to prod/us-east-1 → full rollout to prod/eu-west-1
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

2. Generate your `.gitlab-ci.yml`:

```bash
grunter init
```

3. Open a Merge Request that modifies a Terragrunt unit. Grunter will detect the changes, run plans, and post the results as MR comments.

## Commands

### `grunter orchestrate`

Detect changed Terragrunt units and output a JSON execution plan.

```bash
grunter orchestrate --base <sha> --head <sha> [-o plan.json] [--env dev]
```

| Flag | Default | Env var fallback |
|------|---------|-----------------|
| `--base` | — | `CI_MERGE_REQUEST_DIFF_BASE_SHA` |
| `--head` | — | `CI_COMMIT_SHA` |
| `-o, --output` | stdout | — |
| `--env` | — | — |

When `--env` is provided, only changes under that environment's path are included.

Output:

```json
{
  "environment": "dev",
  "layers": [
    { "units": [{ "path": "envs/dev/us-east-1/vpc", "change_type": "ModuleChanged" }] }
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
  --project-id 123 --mr-iid 1 --gitlab-url https://gitlab.example.com [--env dev]
```

| Flag | Default | Env var fallback |
|------|---------|-----------------|
| `--input` | stdin | — |
| `--unit` | — | — |
| `--action` | `plan` | — |
| `--project-id` | — | `CI_PROJECT_ID` |
| `--mr-iid` | — | `CI_MERGE_REQUEST_IID` |
| `--gitlab-url` | `https://gitlab.com` | `CI_SERVER_URL` |
| `--env` | — | — |

When `--env` is provided, the comment marker includes the environment name to allow per-environment comments.

Requires `GITLAB_TOKEN` or `CI_JOB_TOKEN` environment variable.

Comments are idempotent — re-running updates the existing comment instead of creating a duplicate.

### `grunter drift`

Scan all Terragrunt units and run `plan` on each to detect drift.

```bash
grunter drift [--root .] [-o drift-report.md] [--env dev]
```

When `--env` is provided, drift detection is scoped to that environment's path.

### `grunter envdiff`

Compare two environments to see structural and content divergence.

```bash
grunter envdiff <source> <target> [--json] [-o report.md]
```

Shows:
- **Structural differences**: units that exist in one environment but not the other
- **Content differences**: line-by-line `terragrunt.hcl` diffs for shared units

### `grunter promote`

Generate a progressive (canary-style) deployment plan. Designed to run after merge to the deploy branch.

```bash
grunter promote [--from dev] [--to prod] [--dry-run] [--json] [-o plan.json]
```

| Flag | Default |
|------|---------|
| `--from` | first environment in sequence |
| `--to` | last environment in sequence |
| `--dry-run` | `false` |
| `--json` | `false` |
| `-o, --output` | stdout |

Output:

```json
{
  "stages": [
    {
      "environment": "staging",
      "layers": [{ "units": [{"path": "envs/staging/us-east-1/vpc", "change_type": "ModuleChanged"}] }],
      "is_canary": false
    },
    {
      "environment": "prod",
      "region": "us-east-1",
      "layers": [{ "units": [{"path": "envs/prod/us-east-1/vpc", "change_type": "ModuleChanged"}] }],
      "is_canary": true
    },
    {
      "environment": "prod",
      "region": "eu-west-1",
      "layers": [{ "units": [{"path": "envs/prod/eu-west-1/vpc", "change_type": "ModuleChanged"}] }],
      "is_canary": false
    }
  ]
}
```

For multi-region environments, the first region is the canary (`is_canary: true`). CI pipelines can gate between canary and full rollout.

### `grunter rfc-check`

Verify that a deployment has an approved RFC/change request before proceeding. Used as a gate before environment deployments in CI.

```bash
grunter rfc-check --env staging [--project-id 123] [--mr-iid 1] [--gitlab-url https://gitlab.example.com] [--skip-window]
```

| Flag | Default | Env var fallback |
|------|---------|-----------------|
| `--env` | — | — |
| `--project-id` | — | `CI_PROJECT_ID` |
| `--mr-iid` | — | `CI_MERGE_REQUEST_IID` |
| `--gitlab-url` | `https://gitlab.com` | `CI_SERVER_URL` |
| `--change-number` | — | configured via `change_number_var` in config |
| `--skip-window` | `false` | — |

Requires `GITLAB_TOKEN` or `CI_JOB_TOKEN` for GitLab source, `SERVICENOW_USERNAME` + `SERVICENOW_PASSWORD` for ServiceNow source, or the configured `auth_token_var` for custom sources.

**GitLab source** checks:
- MR has the `approved_label` (e.g. `rfc-approved`)
- If `issue_approved_label` is set, a linked issue has that label (e.g. `status::approved`)
- If `emergency_label` is set and present on the MR, deployment window checks are bypassed

**Post-merge pipelines**: when `CI_MERGE_REQUEST_IID` is unavailable, the checker looks up the MR from `CI_COMMIT_SHA` automatically.

**Environments without RFC config** pass through with exit code 0.

### `grunter init`

Generate a `.gitlab-ci.yml` pipeline configuration from your `.grunter/config.yml`. Produces a complete pipeline with MR planning, progressive deployment, canary gates, and rollback support.

```bash
grunter init [-o .gitlab-ci.yml] [--force]
```

| Flag | Default |
|------|---------|
| `-o, --output` | `.gitlab-ci.yml` |
| `--force` | `false` — refuse to overwrite existing file |

The generated pipeline includes:
- **MR pipeline**: detect changes, run plans, post comments, envdiff
- **Post-merge pipeline**: per-environment generate + rollout stages with child pipelines
- **Approval gates**: manual jobs with `approval_group` membership checks when configured
- **RFC checks**: `grunter rfc-check` runs before deployment when `rfc` is configured
- **Canary gates**: approve/rollback for multi-region environments
- **Release**: creates a GitLab release after all environments are deployed

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

### Environments (optional)

Define environments for env-aware commands. Order defines the promotion sequence.

```yaml
environments:
  - name: dev
    path: envs/dev
    regions:
      - us-east-1
      - eu-west-1
  - name: staging
    path: envs/staging
    regions:
      - us-east-1
  - name: prod
    path: envs/prod
    regions:
      - us-east-1
      - eu-west-1
```

When environments are defined:
- `grunter orchestrate --env dev` filters changes to a single environment
- `grunter drift --env dev` scopes drift detection
- `grunter envdiff dev prod` shows cross-environment differences
- `grunter promote` generates progressive deployment plans with canary stages
- `grunter rfc-check --env staging` verifies RFC approval before deployment

When no environments are defined, all existing commands work unchanged.

### RFC verification (optional)

Add `rfc` to an environment to gate deployments on change request approval.

**GitLab-based** (labels + linked issues):

```yaml
environments:
  - name: staging
    path: envs/staging
    rfc:
      source: gitlab
      approved_label: rfc-approved             # required — MR must have this label
      issue_approved_label: "status::approved"  # optional — a linked issue must have this label
      emergency_label: emergency                # optional — bypasses deployment window
```

**ServiceNow-based**:

```yaml
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: servicenow
      instance_url: https://myco.service-now.com
      change_number_var: SNOW_CHG_NUMBER  # env var containing the change number
```

**Custom API-based** (any HTTP endpoint):

```yaml
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: custom
      url: "https://change-mgmt.example.com/api/v1/changes/{{change_id}}"
      method: GET                        # optional, default GET
      change_number_var: RFC_CHANGE_ID   # env var holding the change identifier
      auth_type: bearer                  # "basic", "bearer", or "header"
      auth_token_var: CHANGE_API_TOKEN   # env var holding the credential
      response_path: data               # optional — dot-path to drill into response
      approved_field: status             # JSON field to check for approval
      approved_value: approved           # value that means approved
      emergency_field: priority          # optional — field for emergency detection
      emergency_value: P1               # value that means emergency
      identifier_field: ticket_number   # optional — field for display identifier
```

`{{change_id}}` in the URL is replaced with the value from `change_number_var` (or `--change-number`). Auth types: `bearer` (Authorization: Bearer), `basic` (HTTP Basic with `auth_username_var`/`auth_token_var`), or `header` (custom header via `auth_header_name`).

### Deployment windows (optional)

Restrict deployments to specific days and times. Emergency RFCs bypass this check.

**Schedule-based** (default):

```yaml
environments:
  - name: prod
    path: envs/prod
    deployment_window:
      days: [monday, tuesday, wednesday, thursday, friday]
      start: "09:00"
      end: "17:00"
      timezone: America/New_York
```

**Custom API-based**:

```yaml
environments:
  - name: prod
    path: envs/prod
    deployment_window:
      source: custom
      url: "https://deploy-gate.example.com/api/v1/windows/current"
      auth_type: bearer
      auth_token_var: DEPLOY_GATE_TOKEN
      response_path: data               # optional — dot-path into response
      allowed_field: is_open             # JSON field — boolean or "true"/"false" string
      message_field: reason              # optional — extracted when not allowed
      next_window_field: next_open_at    # optional — ISO 8601 timestamp
```

### Approval group (optional)

Restrict who can trigger deployments to members of a GitLab group. When `approval_group` is set, manual approval jobs generated by `grunter init` check that the triggering user is a member of the specified group via the GitLab Groups API before allowing the deployment to proceed.

```yaml
environments:
  - name: prod
    path: envs/prod
    approval_group: platform-approvers
```

This applies to all manual gates for the environment: the initial deploy approval, canary approval, and full rollout approval (for multi-region environments). Can be combined with `rfc` and `deployment_window`.

## GitLab CI template

A reference `.gitlab-ci.yml` is provided in `templates/gitlab-ci.yml`. It defines these stages:

| Stage | Trigger | What it does |
|-------|---------|-------------|
| `detect` | MR or push to main | Runs `grunter orchestrate`, outputs execution plan |
| `plan` | MR only | Runs `grunter execute plan` + `grunter comment` per unit |
| `apply` | Push to main only | Runs `grunter execute apply` per unit in dependency order |
| `promote` | Push to main only | (Optional) Progressive deployment with canary stages |

The `.envdiff` and `.promote` jobs are hidden by default (prefixed with `.`). Enable them by removing the dot prefix when your repo uses environments.

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
go test -tags=integration -timeout 10m ./test/ -run TestE2E_GitLabRFCCheck
go test -tags=integration -timeout 10m ./internal/gitlab/
go test -tags=integration -timeout 10m ./internal/rfc/
```

## License

MIT
