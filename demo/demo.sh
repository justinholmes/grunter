#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────────────
# Grunter Demo — Pipeline-Driven Genesys Cloud Deployment
# ──────────────────────────────────────────────────────────────────────────────
#
# Demonstrates grunter managing multi-region Genesys Cloud contact center
# infrastructure entirely through GitLab CI pipelines:
#
#   MR Pipeline:   change detection -> terragrunt plan -> MR comments
#   Post-Merge:    progressive deployment with canary gates
#
# Infrastructure layout:
#   staging/eu-west-1          — staging environment (source)
#   production/eu-west-1       — prod EU-West (canary)
#   production/eu-central-1    — prod EU-Central
#   production/ap-southeast-1  — prod APAC
#
# Prerequisites:
#   - GitLab CE running on port 8929 (root / testpassword123!)
#   - go, tofu, terragrunt, curl, jq installed
#
# Usage:
#   bash demo/demo.sh
# ──────────────────────────────────────────────────────────────────────────────
set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────
GITLAB_URL="http://localhost:8929"
GITLAB_USER="root"
GITLAB_PASSWORD="testpassword123!"
PROJECT_NAME="genesys-cloud-infra"
PROJECT_PATH="root/$PROJECT_NAME"
TOKEN_VALUE=""
GRUNTER_REPO="$(cd "$(dirname "$0")/.." && pwd)"

WORK_DIR=""
PROJECT_ID=""
MR_IID=""
MR_PIPELINE_1_ID=""
RUNNER_PID=""
RUNNER_AUTH_TOKEN=""
RUNNER_CONFIG=""
BASELINE_PIPELINE_ID=""

# ── Colors ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

# ── Output Helpers ───────────────────────────────────────────────────────────
section() {
  echo ""
  echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
  echo -e "${BOLD}${BLUE}  $1${NC}"
  echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
  echo ""
}

info()     { echo -e "  ${CYAN}[INFO]${NC} $*"; }
success()  { echo -e "  ${GREEN}[OK]${NC}   $*"; }
warn()     { echo -e "  ${YELLOW}[WARN]${NC} $*"; }
die()      { echo -e "  ${RED}[ERR]${NC}  $*" >&2; exit 1; }
run_cmd()  { echo -e "\n  ${DIM}\$ $*${NC}"; eval "$@"; }
show_file(){ echo -e "  ${DIM}── $1 ──${NC}"; sed 's/^/    /' "$1"; echo ""; }

pause() {
  echo ""
  echo -e "  ${YELLOW}>>> $1${NC}"
  if [ -t 0 ]; then
    read -r -p "  Press Enter to continue... "
  else
    sleep 1
  fi
  echo ""
}

# ── GitLab API ───────────────────────────────────────────────────────────────
gitlab_api() {
  local method=$1 endpoint=$2
  shift 2
  local resp http_code body
  resp=$(curl -s -w "\n%{http_code}" \
    -X "$method" \
    -H "PRIVATE-TOKEN: $TOKEN_VALUE" \
    "${GITLAB_URL}/api/v4${endpoint}" \
    "$@")
  http_code=$(echo "$resp" | tail -1)
  body=$(echo "$resp" | sed '$d')

  if [[ "$http_code" -ge 400 ]]; then
    echo "API error ${http_code}: ${body}" >&2
    return 1
  fi
  echo "$body"
}

# ── Pipeline Helpers ─────────────────────────────────────────────────────────
urlencode() { echo "$1" | sed 's|/|%2F|g'; }

wait_for_pipeline_on_ref() {
  local ref=$1
  local source_filter=${2:-}
  local after_id=${3:-0}
  local encoded_ref
  encoded_ref=$(urlencode "$ref")
  info "Waiting for pipeline on ref=$ref ..." >&2
  for _ in $(seq 1 90); do
    local pipelines
    pipelines=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines?ref=${encoded_ref}&per_page=5&sort=desc&order_by=id" 2>/dev/null || echo "[]")
    local pipeline_id
    if [ -n "$source_filter" ]; then
      pipeline_id=$(echo "$pipelines" | jq -r "[.[] | select(.source == \"$source_filter\" and .id > $after_id)] | .[0].id // empty")
    else
      pipeline_id=$(echo "$pipelines" | jq -r "[.[] | select(.id > $after_id)] | .[0].id // empty")
    fi
    if [ -n "$pipeline_id" ] && [ "$pipeline_id" != "null" ]; then
      success "Pipeline #$pipeline_id started" >&2
      echo "$pipeline_id"
      return 0
    fi
    sleep 2
  done
  die "Timed out waiting for pipeline on ref=$ref"
}

wait_for_pipeline_completion() {
  local pipeline_id=$1
  local max_wait=${2:-600}
  local start_time
  start_time=$(date +%s)

  while true; do
    local status
    status=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id" | jq -r '.status')
    case "$status" in
      success)
        echo ""
        success "Pipeline #$pipeline_id completed successfully"
        return 0
        ;;
      failed)
        echo ""
        warn "Pipeline #$pipeline_id failed"
        local failed_jobs
        failed_jobs=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id/jobs?scope[]=failed&per_page=20" 2>/dev/null | jq -r '.[].name' 2>/dev/null || true)
        if [ -n "$failed_jobs" ]; then
          echo "  Failed jobs:"
          echo "$failed_jobs" | while read -r j; do echo "    - $j"; done
        fi
        return 1
        ;;
      canceled)
        echo ""
        info "Pipeline #$pipeline_id canceled"
        return 0
        ;;
      manual)
        echo ""
        return 2
        ;;
      *)
        local elapsed=$(( $(date +%s) - start_time ))
        if [ "$elapsed" -ge "$max_wait" ]; then
          echo ""
          die "Pipeline #$pipeline_id timed out after ${max_wait}s (status: $status)"
        fi
        printf "\r  Pipeline #%s: %-20s (%ds)  " "$pipeline_id" "$status" "$elapsed"
        sleep 5
        ;;
    esac
  done
}

play_next_approve_job() {
  local pipeline_id=$1

  local jobs
  jobs=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id/jobs?per_page=100&scope[]=manual" 2>/dev/null || echo "[]")

  local job
  job=$(echo "$jobs" | jq -r '[.[] | select(.name | startswith("approve:"))] | sort_by(.id) | .[0] // empty')

  if [ -z "$job" ] || [ "$job" = "null" ] || [ "$job" = "" ]; then
    return 1
  fi

  local job_id job_name
  job_id=$(echo "$job" | jq -r '.id')
  job_name=$(echo "$job" | jq -r '.name')

  echo ""
  case "$job_name" in
    "approve:production-deploy")
      info "Gate: Production deployment approval"
      info "  In production this would run RFC check + group membership validation."
      info "  After approval, canary deploys to eu-west-1 first."
      ;;
    "approve:production-canary")
      info "Gate: Production canary approval"
      info "  Canary region (eu-west-1) deployed successfully."
      info "  Approve to roll out to eu-central-1 and ap-southeast-1."
      ;;
    "approve:production-rollout")
      info "Gate: Production full rollout"
      info "  Approve to deploy remaining regions (EU-Central + APAC)."
      ;;
    *)
      info "Manual gate: $job_name"
      ;;
  esac

  pause "Approve '$job_name'?"

  gitlab_api POST "/projects/$PROJECT_ID/jobs/$job_id/play" > /dev/null
  success "Approved: $job_name"
  return 0
}

drive_deployment_pipeline() {
  local pipeline_id=$1
  local pipeline_url="$GITLAB_URL/$PROJECT_PATH/-/pipelines/$pipeline_id"
  local manual_wait=0

  info "Deployment pipeline: $pipeline_url"
  echo ""

  while true; do
    local status
    status=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id" | jq -r '.status')
    case "$status" in
      success)
        echo ""
        success "Deployment pipeline completed!"
        return 0
        ;;
      failed)
        echo ""
        warn "Deployment pipeline failed — check $pipeline_url"
        return 1
        ;;
      canceled)
        echo ""
        info "Pipeline canceled"
        return 0
        ;;
      manual)
        if ! play_next_approve_job "$pipeline_id"; then
          manual_wait=$((manual_wait + 1))
          if [ "$manual_wait" -ge 60 ]; then
            die "Pipeline stuck in manual state for 5 minutes — check $pipeline_url"
          fi
          sleep 5
        else
          manual_wait=0
        fi
        ;;
      *)
        manual_wait=0
        printf "\r  Pipeline #%s: %-20s" "$pipeline_id" "$status"
        sleep 5
        ;;
    esac
  done
}

show_mr_notes() {
  local mr_iid=$1
  info "MR plan comments:"
  local notes
  notes=$(gitlab_api GET "/projects/$PROJECT_ID/merge_requests/$mr_iid/notes?per_page=100&sort=asc")
  # Show non-system notes (plan comments posted by grunter)
  local count
  count=$(echo "$notes" | jq '[.[] | select(.system == false)] | length')
  if [ "$count" -eq 0 ]; then
    info "  (no comments yet)"
    return
  fi
  echo "$notes" | jq -r '.[] | select(.system == false) | "────────────────────────────────────────\n\(.body)" ' | head -200 || true
  if [ "$count" -gt 0 ]; then
    echo ""
    info "  $count plan comment(s) on MR"
  fi
}

show_job_log_excerpt() {
  local pipeline_id=$1 job_name=$2 lines=${3:-30}
  local job_id
  job_id=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id/jobs?per_page=100" | \
    jq -r ".[] | select(.name == \"$job_name\") | .id" | head -1)
  if [ -n "$job_id" ] && [ "$job_id" != "null" ]; then
    info "Log excerpt from '$job_name':"
    gitlab_api GET "/projects/$PROJECT_ID/jobs/$job_id/trace" 2>/dev/null | \
      sed 's/\x1b\[[0-9;]*m//g' | tail -"$lines" | sed 's/^/    /'
  fi
}

# ── Infrastructure Creation ──────────────────────────────────────────────────

create_root_terragrunt() {
  local dir=$1
  cat > "$dir/root.hcl" << 'TGEOF'
remote_state {
  backend = "local"
  config = {
    path = "${get_terragrunt_dir()}/terraform.tfstate"
  }
}

generate "provider" {
  path      = "provider.tf"
  if_exists = "overwrite"
  contents  = <<EOF
terraform {
  backend "local" {}
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
}
EOF
}
TGEOF
}

create_grunter_config() {
  local dir=$1
  mkdir -p "$dir/.grunter"
  cat > "$dir/.grunter/config.yml" << 'EOF'
deploy_branch: main
tf_binary: tofu
tg_binary: terragrunt
ignore:
  - "**/.terraform.lock.hcl"
  - "**/README.md"

environments:
  - name: staging
    path: envs/staging
    regions:
      - eu-west-1
  - name: production
    path: envs/production
    regions:
      - eu-west-1
      - eu-central-1
      - ap-southeast-1
EOF
}

# create_unit DIR MODULE_SOURCE ENV REGION [dep_path ...]
create_unit() {
  local dir=$1 module_source=$2 env=$3 region=$4
  shift 4
  mkdir -p "$dir"
  {
    echo 'include "root" {'
    echo '  path = find_in_parent_folders("root.hcl")'
    echo '}'
    echo ""
    echo "terraform {"
    echo "  source = \"$module_source\""
    echo "}"
    if [ $# -gt 0 ]; then
      echo ""
      echo "dependencies {"
      echo -n "  paths = ["
      local first=true
      while [ $# -ge 1 ]; do
        if [ "$first" = true ]; then first=false; else echo -n ", "; fi
        echo -n "\"$1\""
        shift
      done
      echo "]"
      echo "}"
    fi
    echo ""
    echo "inputs = {"
    echo "  environment = \"$env\""
    echo "  region      = \"$region\""
    echo "}"
  } > "$dir/terragrunt.hcl"
}

create_modules() {
  local base=$1

  # modules/routing/queues
  mkdir -p "$base/modules/routing/queues"
  cat > "$base/modules/routing/queues/main.tf" << 'MODEOF'
variable "environment" { type = string }
variable "region"      { type = string }

resource "null_resource" "queue_sales" {
  triggers = {
    name                    = "Sales"
    description             = "Inbound sales inquiries"
    acw_timeout_ms          = "30000"
    skill_evaluation_method = "BEST"
    enable_transcription    = "true"
  }
}

resource "null_resource" "queue_support" {
  triggers = {
    name                    = "Support"
    description             = "Technical support requests"
    acw_timeout_ms          = "30000"
    skill_evaluation_method = "ALL"
    enable_transcription    = "true"
  }
}

resource "null_resource" "queue_billing" {
  triggers = {
    name                    = "Billing"
    description             = "Billing and account inquiries"
    acw_timeout_ms          = "15000"
    skill_evaluation_method = "BEST"
    enable_transcription    = "true"
  }
}
MODEOF

  # modules/routing/skills
  mkdir -p "$base/modules/routing/skills"
  cat > "$base/modules/routing/skills/main.tf" << 'MODEOF'
variable "environment" { type = string }
variable "region"      { type = string }

resource "null_resource" "skill_1" {
  triggers = { name = "English", type = "language" }
}

resource "null_resource" "skill_2" {
  triggers = { name = "French", type = "language" }
}

resource "null_resource" "skill_3" {
  triggers = { name = "Premium", type = "custom" }
}
MODEOF

  # modules/flows/inbound-call
  mkdir -p "$base/modules/flows/inbound-call"
  cat > "$base/modules/flows/inbound-call/main.tf" << 'MODEOF'
variable "environment" { type = string }
variable "region"      { type = string }

resource "null_resource" "main_ivr" {
  triggers = {
    name    = "Main IVR"
    type    = "INBOUNDCALL"
    version = "1.0"
  }
}
MODEOF

  # modules/users/agents
  mkdir -p "$base/modules/users/agents"
  cat > "$base/modules/users/agents/main.tf" << 'MODEOF'
variable "environment" { type = string }
variable "region"      { type = string }

resource "null_resource" "agent_group" {
  triggers = {
    name       = "Contact Center Agents"
    visibility = "PUBLIC"
  }
}
MODEOF
}

create_region() {
  local base=$1 env=$2 region=$3
  local rd="$base/envs/$env/$region"
  local depth
  # Calculate relative path from env dir back to modules/
  # e.g. envs/staging/eu-west-1/routing/queues -> ../../../../../modules/routing/queues
  depth="../../../../.."

  # routing/queues — no dependencies
  create_unit "$rd/routing/queues" "$depth/modules/routing/queues" "$env" "$region"

  # routing/skills — no dependencies
  create_unit "$rd/routing/skills" "$depth/modules/routing/skills" "$env" "$region"

  # flows/inbound-call — depends on queues
  create_unit "$rd/flows/inbound-call" "$depth/modules/flows/inbound-call" "$env" "$region" \
    "../../routing/queues"

  # users/agents — depends on queues + skills
  create_unit "$rd/users/agents" "$depth/modules/users/agents" "$env" "$region" \
    "../../routing/queues" \
    "../../routing/skills"
}

# ── Cleanup ──────────────────────────────────────────────────────────────────
cleanup() {
  echo ""
  info "Cleaning up..."

  if [ -n "${RUNNER_PID:-}" ]; then
    kill "$RUNNER_PID" 2>/dev/null || true
    wait "$RUNNER_PID" 2>/dev/null || true
    info "  Runner process stopped"
  fi

  if [ -n "${RUNNER_CONFIG:-}" ] && [ -f "$RUNNER_CONFIG" ]; then
    gitlab-runner unregister --config "$RUNNER_CONFIG" --all-runners 2>/dev/null || true
    info "  Runner unregistered"
  fi

  if [ -n "${WORK_DIR:-}" ] && [ -d "$WORK_DIR" ]; then
    rm -rf "$WORK_DIR" "${WORK_DIR}/../grunter-demo-builds"
    info "  Temp directory removed"
  fi

  success "Cleanup complete"
}
trap cleanup EXIT

# ══════════════════════════════════════════════════════════════════════════════
#  DEMO STEPS
# ══════════════════════════════════════════════════════════════════════════════

step0_prerequisites() {
  section "Step 0: Prerequisites"

  # Ensure go, tofu, terragrunt are on PATH
  export PATH="$HOME/.local/bin:$HOME/.go/bin:$HOME/.go/current/bin:$PATH"

  local missing=()
  for cmd in git curl jq go tofu terragrunt; do
    if command -v "$cmd" &>/dev/null; then
      success "$cmd: $(command -v "$cmd")"
    else
      missing+=("$cmd")
    fi
  done

  if [ ${#missing[@]} -gt 0 ]; then
    die "Missing: ${missing[*]}"
  fi

  # Check GitLab is reachable
  if ! curl -s --max-time 5 "$GITLAB_URL/-/health" > /dev/null 2>&1; then
    die "GitLab not reachable at $GITLAB_URL"
  fi
  success "GitLab CE reachable at $GITLAB_URL"

  # Check / download gitlab-runner
  if ! command -v gitlab-runner &>/dev/null; then
    info "Downloading gitlab-runner..."
    mkdir -p "$HOME/.local/bin"
    curl -L --output "$HOME/.local/bin/gitlab-runner" \
      "https://s3.dualstack.us-east-1.amazonaws.com/gitlab-runner-downloads/latest/binaries/gitlab-runner-linux-amd64"
    chmod +x "$HOME/.local/bin/gitlab-runner"
  fi
  success "gitlab-runner: $(command -v gitlab-runner)"
}

step1_build_grunter() {
  section "Step 1: Build grunter from source"

  run_cmd "cd '$GRUNTER_REPO' && go build -o '$HOME/.local/bin/grunter' ."
  success "grunter built: $HOME/.local/bin/grunter"
}

step2_gitlab_setup() {
  section "Step 2: GitLab authentication & runner setup"

  info "Creating API token..."

  # Try to validate existing token (from a previous demo run)
  if [ -n "$TOKEN_VALUE" ] && gitlab_api GET "/user" > /dev/null 2>&1; then
    success "Existing token still valid"
  else
    # Get an OAuth token using the resource owner password grant
    info "Authenticating via OAuth..."
    local oauth_resp
    oauth_resp=$(curl -s -X POST "${GITLAB_URL}/oauth/token" \
      -d "grant_type=password" \
      --data-urlencode "username=${GITLAB_USER}" \
      --data-urlencode "password=${GITLAB_PASSWORD}")
    local oauth_token
    oauth_token=$(echo "$oauth_resp" | jq -r '.access_token // empty')
    if [ -z "$oauth_token" ]; then
      die "OAuth login failed: $oauth_resp"
    fi
    success "OAuth token obtained"

    # Create a personal access token via the API
    # Note: we don't revoke old tokens — previous pipelines may still be using them.
    info "Creating personal access token..."
    local expires_at
    expires_at=$(date -d "+30 days" +%Y-%m-%d)
    local pat_resp
    pat_resp=$(curl -s -X POST \
      -H "Authorization: Bearer $oauth_token" \
      "${GITLAB_URL}/api/v4/users/1/personal_access_tokens" \
      -d "name=grunter-demo-token" \
      -d "scopes[]=api" \
      -d "scopes[]=read_user" \
      -d "scopes[]=read_repository" \
      -d "scopes[]=write_repository" \
      -d "expires_at=${expires_at}")
    TOKEN_VALUE=$(echo "$pat_resp" | jq -r '.token // empty')
    if [ -z "$TOKEN_VALUE" ]; then
      die "PAT creation failed: $pat_resp"
    fi

    # Validate
    gitlab_api GET "/user" > /dev/null 2>&1 || die "Token validation failed"
    success "Token created and validated"
  fi

  # Delete existing project if present
  local existing_project
  existing_project=$(gitlab_api GET "/projects?search=$PROJECT_NAME" 2>/dev/null | jq -r ".[0].id // empty")
  if [ -n "$existing_project" ] && [ "$existing_project" != "null" ]; then
    info "Deleting existing project #$existing_project..."
    gitlab_api DELETE "/projects/$existing_project" > /dev/null 2>&1 || true
    sleep 3
  fi

  # Create work directory
  WORK_DIR=$(mktemp -d /tmp/grunter-demo-XXXXXX)
  success "Work directory: $WORK_DIR"

  # Register a shell runner
  info "Registering GitLab runner..."
  RUNNER_CONFIG="$WORK_DIR/runner-config.toml"

  local runner_resp
  runner_resp=$(gitlab_api POST "/user/runners" \
    --data-urlencode "runner_type=instance_type" \
    --data-urlencode "description=grunter-demo-runner" \
    --data-urlencode "run_untagged=true" \
    --data-urlencode "access_level=not_protected")
  RUNNER_AUTH_TOKEN=$(echo "$runner_resp" | jq -r '.token')

  if [ -z "$RUNNER_AUTH_TOKEN" ] || [ "$RUNNER_AUTH_TOKEN" = "null" ]; then
    die "Failed to create runner: $runner_resp"
  fi

  gitlab-runner register \
    --non-interactive \
    --url "$GITLAB_URL" \
    --token "$RUNNER_AUTH_TOKEN" \
    --executor shell \
    --builds-dir "$WORK_DIR/../grunter-demo-builds" \
    --clone-url "$GITLAB_URL" \
    --config "$RUNNER_CONFIG" \
    --env "PATH=$HOME/.local/bin:$HOME/.go/bin:$HOME/.go/current/bin:/usr/local/bin:/usr/bin:/bin" 2>/dev/null

  # Start runner in background
  gitlab-runner run --config "$RUNNER_CONFIG" > "$WORK_DIR/runner.log" 2>&1 &
  RUNNER_PID=$!
  success "Runner started (PID: $RUNNER_PID)"
}

step3_create_infrastructure() {
  section "Step 3: Create Genesys Cloud infrastructure"

  info "Creating multi-region contact center structure..."

  create_root_terragrunt "$WORK_DIR"
  create_grunter_config "$WORK_DIR"
  create_modules "$WORK_DIR"

  # Staging — EU West (single region)
  create_region "$WORK_DIR" staging eu-west-1

  # Production — 3 regions (canary = eu-west-1)
  create_region "$WORK_DIR" production eu-west-1
  create_region "$WORK_DIR" production eu-central-1
  create_region "$WORK_DIR" production ap-southeast-1

  success "Infrastructure files created"

  # Show directory tree
  info "Directory structure:"
  (cd "$WORK_DIR" && find modules envs -type f | sort | sed 's/^/    /')

  pause "Review the infrastructure layout"

  # Show key files
  show_file "$WORK_DIR/.grunter/config.yml"
  show_file "$WORK_DIR/modules/routing/queues/main.tf"
  show_file "$WORK_DIR/envs/staging/eu-west-1/routing/queues/terragrunt.hcl"
  show_file "$WORK_DIR/envs/staging/eu-west-1/flows/inbound-call/terragrunt.hcl"

  info "Dependency chain: queues/skills -> flows -> agents"

  # Generate pipeline with grunter init
  info "Generating GitLab CI pipeline..."
  (cd "$WORK_DIR" && grunter init -c .grunter/config.yml --force)
  success "Generated .gitlab-ci.yml"
  show_file "$WORK_DIR/.gitlab-ci.yml"

  pause "Review the generated pipeline"
}

step4_push_baseline() {
  section "Step 4: Push baseline to GitLab"

  # Init git repo
  (cd "$WORK_DIR" && \
    git init -b main && \
    git config user.email "demo@example.com" && \
    git config user.name "Grunter Demo" && \
    git add -A && \
    git commit -m "Initial Genesys Cloud infrastructure" \
  ) > /dev/null 2>&1
  success "Git repo initialized with baseline commit"

  # Create GitLab project
  local project_resp
  project_resp=$(gitlab_api POST "/projects" \
    --data-urlencode "name=$PROJECT_NAME" \
    --data-urlencode "visibility=internal" \
    --data-urlencode "initialize_with_readme=false")
  PROJECT_ID=$(echo "$project_resp" | jq -r '.id')
  success "GitLab project created: $GITLAB_URL/$PROJECT_PATH (ID: $PROJECT_ID)"

  # Set CI variables
  gitlab_api POST "/projects/$PROJECT_ID/variables" \
    -d "key=GITLAB_TOKEN&value=$TOKEN_VALUE" > /dev/null 2>&1
  gitlab_api POST "/projects/$PROJECT_ID/variables" \
    -d "key=GIT_DEPTH&value=0" > /dev/null 2>&1
  gitlab_api POST "/projects/$PROJECT_ID/variables" \
    -d "key=CI_SERVER_URL&value=$GITLAB_URL" > /dev/null 2>&1
  success "CI variables configured (GITLAB_TOKEN, GIT_DEPTH, CI_SERVER_URL)"

  # Push to main
  (cd "$WORK_DIR" && \
    git remote add origin "http://oauth2:${TOKEN_VALUE}@localhost:8929/${PROJECT_PATH}.git" && \
    git push -u origin main \
  ) > /dev/null 2>&1
  success "Pushed to $GITLAB_URL/$PROJECT_PATH"

  local BASE_SHA
  BASE_SHA=$(cd "$WORK_DIR" && git rev-parse HEAD)
  info "Base SHA: $BASE_SHA"

  # A post-merge pipeline will start — we'll let it sit at the manual gate
  info "Post-merge pipeline triggered (will pause at approval gate)"
  sleep 5
  BASELINE_PIPELINE_ID=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines?ref=main&per_page=1&sort=desc&order_by=id" | jq -r '.[0].id // empty')
  if [ -n "$BASELINE_PIPELINE_ID" ] && [ "$BASELINE_PIPELINE_ID" != "null" ]; then
    info "Baseline pipeline #$BASELINE_PIPELINE_ID — letting it run in background"
  fi

  pause "Baseline pushed. Next: create a feature branch"
}

step5_feature_branch() {
  section "Step 5: Feature branch — add Premium Support queue"

  (cd "$WORK_DIR" && git checkout -b feature/add-premium-support-queue) > /dev/null 2>&1
  success "Branch: feature/add-premium-support-queue"

  # 1. Add Premium Support queue to the queues module
  info "Adding Premium Support queue to module..."
  cat >> "$WORK_DIR/modules/routing/queues/main.tf" << 'MODEOF'

resource "null_resource" "queue_premium_support" {
  triggers = {
    name                      = "Premium Support"
    description               = "Priority support for enterprise customers"
    acw_timeout_ms            = "30000"
    skill_evaluation_method   = "BEST"
    enable_transcription      = "true"
    service_level_pct         = "0.95"
    service_level_duration_ms = "10000"
  }
}
MODEOF

  # 2. Update IVR flow version in the flow module
  info "Updating IVR flow to route premium customers..."
  sed -i 's/version = "1.0"/version = "2.0"/' \
    "$WORK_DIR/modules/flows/inbound-call/main.tf"

  # 3. Add premium agent to the agents module
  info "Adding premium support agent..."
  cat >> "$WORK_DIR/modules/users/agents/main.tf" << 'MODEOF'

resource "null_resource" "premium_agent" {
  triggers = {
    name       = "Premium Support Team"
    visibility = "MEMBERS"
    skill      = "Premium"
  }
}
MODEOF

  # Commit
  (cd "$WORK_DIR" && git add -A && \
    git commit -m "Add premium support queue, update IVR, add premium agent") > /dev/null 2>&1
  success "Changes committed"

  run_cmd "cd '$WORK_DIR' && git diff --stat main"

  pause "Changes ready. Next: push and create MR"
}

step6_create_mr() {
  section "Step 6: Push to GitLab & create Merge Request"

  (cd "$WORK_DIR" && git push -u origin feature/add-premium-support-queue) > /dev/null 2>&1
  success "Branch pushed"

  # Create MR
  local mr_resp
  mr_resp=$(gitlab_api POST "/projects/$PROJECT_ID/merge_requests" \
    --data-urlencode "source_branch=feature/add-premium-support-queue" \
    --data-urlencode "target_branch=main" \
    --data-urlencode "title=Add premium support queue to contact center")
  MR_IID=$(echo "$mr_resp" | jq -r '.iid')
  local mr_url
  mr_url=$(echo "$mr_resp" | jq -r '.web_url')
  success "MR !$MR_IID created: $mr_url"

  pause "MR created. Pipeline will run: detect -> plan -> comment"
}

step7_watch_mr_pipeline() {
  section "Step 7: Watch MR pipeline (detect -> plan -> comment)"

  # Wait for MR pipeline to appear
  local pipeline_id
  pipeline_id=$(wait_for_pipeline_on_ref "feature/add-premium-support-queue" "merge_request_event")
  MR_PIPELINE_1_ID="$pipeline_id"

  local pipeline_url="$GITLAB_URL/$PROJECT_PATH/-/pipelines/$pipeline_id"
  info "MR pipeline: $pipeline_url"

  # Wait for completion
  wait_for_pipeline_completion "$pipeline_id" 300 || true

  # Show plan output from the detect job
  show_job_log_excerpt "$pipeline_id" "detect" 15
  echo ""

  # Show plan output from the plan job
  show_job_log_excerpt "$pipeline_id" "plan" 40
  echo ""

  # Show MR comments
  show_mr_notes "$MR_IID"

  info "MR URL: $GITLAB_URL/$PROJECT_PATH/-/merge_requests/$MR_IID"
  pause "Review MR comments. Next: push an update and watch re-plan"
}

step8_push_update() {
  section "Step 8: Push update — increase premium queue ACW timeout"

  # Modify only the premium support queue (address range: from queue_premium_support to closing brace)
  sed -i '/queue_premium_support/,/^}/s/acw_timeout_ms            = "30000"/acw_timeout_ms            = "45000"/' \
    "$WORK_DIR/modules/routing/queues/main.tf"

  (cd "$WORK_DIR" && git add -A && \
    git commit -m "Increase premium queue ACW timeout to 45s") > /dev/null 2>&1
  (cd "$WORK_DIR" && git push) > /dev/null 2>&1
  success "Update pushed"

  info "New pipeline will run — existing MR comments will be updated in-place"
}

step9_watch_replan() {
  section "Step 9: Watch re-plan pipeline"

  sleep 5
  local pipeline_id
  pipeline_id=$(wait_for_pipeline_on_ref "feature/add-premium-support-queue" "merge_request_event" "$MR_PIPELINE_1_ID")

  local pipeline_url="$GITLAB_URL/$PROJECT_PATH/-/pipelines/$pipeline_id"
  info "Re-plan pipeline: $pipeline_url"

  wait_for_pipeline_completion "$pipeline_id" 300 || true

  # Show updated comments
  show_mr_notes "$MR_IID"
  info "Comments updated in-place (not duplicated)"

  # Show envdiff artifact if available
  local envdiff_job_id
  envdiff_job_id=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id/jobs?per_page=100" | \
    jq -r '.[] | select(.name == "envdiff") | .id' 2>/dev/null || true)
  if [ -n "$envdiff_job_id" ] && [ "$envdiff_job_id" != "null" ]; then
    echo ""
    info "Environment diff (staging vs production):"
    show_job_log_excerpt "$pipeline_id" "envdiff" 30
  fi

  pause "Re-plan complete. Next: merge and watch deployment pipeline"
}

step10_merge_and_deploy() {
  section "Step 10: Merge MR & progressive deployment"

  # Cancel the baseline post-merge pipeline if still running
  if [ -n "$BASELINE_PIPELINE_ID" ] && [ "$BASELINE_PIPELINE_ID" != "null" ]; then
    gitlab_api POST "/projects/$PROJECT_ID/pipelines/$BASELINE_PIPELINE_ID/cancel" > /dev/null 2>&1 || true
    info "Canceled baseline pipeline #$BASELINE_PIPELINE_ID"
  fi

  # Merge the MR
  info "Merging MR !$MR_IID..."
  gitlab_api PUT "/projects/$PROJECT_ID/merge_requests/$MR_IID/merge" > /dev/null 2>&1
  success "MR merged"

  # Wait for post-merge pipeline
  sleep 5
  local pipeline_id
  pipeline_id=$(wait_for_pipeline_on_ref "main" "" "${BASELINE_PIPELINE_ID:-0}")

  info "Post-merge deployment pipeline started"
  echo ""
  info "Pipeline flow:"
  info "  1. generate-production     (compare staging vs production)"
  info "  2. approve:production-deploy  (manual gate)"
  info "  3. rollout-production-canary  (deploy eu-west-1 via child pipeline)"
  info "  4. canary gate             (approve or rollback)"
  info "  5. rollout-production      (deploy eu-central-1 + ap-southeast-1)"
  info "  6. create-release          (tag the deployment)"
  echo ""

  pause "Walk through the progressive deployment"

  drive_deployment_pipeline "$pipeline_id"
}

step11_summary() {
  section "Step 11: Summary"

  echo -e "  ${BOLD}What we demonstrated:${NC}"
  echo ""
  echo "  1. Multi-region Genesys Cloud infrastructure managed with terragrunt"
  echo "     - Shared modules in modules/ sourced by each environment"
  echo "     - staging (eu-west-1) and production (eu-west-1, eu-central-1, ap-southeast-1)"
  echo "     - Dependency ordering: queues/skills -> flows -> agents"
  echo ""
  echo "  2. Pipeline generation with 'grunter init'"
  echo "     - MR pipeline: detect -> plan -> comment"
  echo "     - Post-merge pipeline: promote -> canary -> full rollout"
  echo ""
  echo "  3. MR pipeline (all through GitLab CI)"
  echo "     - Automatic change detection across environments"
  echo "     - Terragrunt plan for each changed unit"
  echo "     - Plan comments posted to MR (updated in-place on re-push)"
  echo "     - Environment diff between staging and production"
  echo ""
  echo "  4. Progressive deployment with canary gates"
  echo "     - 'grunter promote' generates child pipeline YAML"
  echo "     - Canary region (eu-west-1) deploys first"
  echo "     - Manual approval gate after canary validation"
  echo "     - Remaining regions (eu-central-1, ap-southeast-1) roll out"
  echo "     - Automatic release tagging on completion"
  echo ""
  echo -e "  ${BOLD}Key grunter commands (all run inside the pipeline):${NC}"
  echo ""
  echo "    grunter orchestrate     Detect changes, output execution plan"
  echo "    grunter execute plan    Run terragrunt plan on a unit"
  echo "    grunter comment         Post plan output to MR"
  echo "    grunter promote         Generate deployment plan + child pipeline YAML"
  echo "    grunter envdiff         Compare environment structures"
  echo "    grunter init            Generate the .gitlab-ci.yml that ties it all together"
  echo ""

  pause "Demo complete"
}

# ══════════════════════════════════════════════════════════════════════════════
#  MAIN
# ══════════════════════════════════════════════════════════════════════════════
main() {
  echo ""
  echo -e "${BOLD}${CYAN}"
  echo "   ___                  _"
  echo "  / _ \\_ __ _   _ _ __ | |_ ___ _ __"
  echo " / /_\\/ '__| | | | '_ \\| __/ _ \\ '__|"
  echo "/ /_\\\\| |  | |_| | | | | ||  __/ |"
  echo "\\____/|_|   \\__,_|_| |_|\\__\\___|_|"
  echo ""
  echo -e "  Pipeline-Driven Genesys Cloud Deployment Demo${NC}"
  echo ""

  step0_prerequisites
  step1_build_grunter
  step2_gitlab_setup
  step3_create_infrastructure
  step4_push_baseline
  step5_feature_branch
  step6_create_mr
  step7_watch_mr_pipeline
  step8_push_update
  step9_watch_replan
  step10_merge_and_deploy
  step11_summary
}

main "$@"
