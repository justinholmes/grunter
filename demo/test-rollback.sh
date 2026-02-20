#!/usr/bin/env bash
# Test: canary rollback path
# Reuses the demo's GitLab container and infrastructure setup,
# drives the pipeline to the canary gate, then plays rollback
# instead of approve to verify the rollback path works.
set -euo pipefail

GITLAB_URL="http://localhost:8929"
GITLAB_USER="root"
GITLAB_PASSWORD="testpassword123!"
PROJECT_NAME="rollback-test"
PROJECT_PATH="root/$PROJECT_NAME"
TOKEN_VALUE=""
GRUNTER_REPO="$(cd "$(dirname "$0")/.." && pwd)"

CONTAINER_NAME="grunter-demo-gitlab"
CONTAINER_RUNTIME=""
STARTED_CONTAINER=""

WORK_DIR=""
PROJECT_ID=""
RUNNER_PID=""
RUNNER_AUTH_TOKEN=""
RUNNER_CONFIG=""

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; DIM='\033[2m'; NC='\033[0m'

info()    { echo -e "  ${CYAN}[INFO]${NC} $*"; }
success() { echo -e "  ${GREEN}[OK]${NC}   $*"; }
warn()    { echo -e "  ${YELLOW}[WARN]${NC} $*"; }
die()     { echo -e "  ${RED}[ERR]${NC}  $*" >&2; exit 1; }

# Source shared functions from demo
detect_container_runtime() {
  if command -v docker &>/dev/null; then
    CONTAINER_RUNTIME="docker"
  elif command -v podman &>/dev/null; then
    CONTAINER_RUNTIME="podman"
  else
    die "Neither docker nor podman found"
  fi
}

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

cleanup() {
  echo ""
  info "Cleaning up..."
  if [ -n "${RUNNER_PID:-}" ]; then
    kill "$RUNNER_PID" 2>/dev/null || true
    wait "$RUNNER_PID" 2>/dev/null || true
  fi
  if [ -n "${RUNNER_CONFIG:-}" ] && [ -f "$RUNNER_CONFIG" ]; then
    gitlab-runner unregister --config "$RUNNER_CONFIG" --all-runners 2>/dev/null || true
  fi
  if [ -n "${WORK_DIR:-}" ] && [ -d "$WORK_DIR" ]; then
    rm -rf "$WORK_DIR" "${WORK_DIR}/../rollback-test-builds"
  fi
  success "Cleanup complete"
}
trap cleanup EXIT

wait_for_pipeline_completion() {
  local pipeline_id=$1 timeout=${2:-300}
  local start_time
  start_time=$(date +%s)
  while true; do
    local elapsed=$(( $(date +%s) - start_time ))
    if [ "$elapsed" -ge "$timeout" ]; then
      warn "Pipeline #$pipeline_id timed out after ${timeout}s"
      return 1
    fi
    local status
    status=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id" | jq -r '.status')
    printf "\r  Pipeline #%s: %-20s (%ds)" "$pipeline_id" "$status" "$elapsed"
    case "$status" in
      success) echo ""; return 0 ;;
      failed)  echo ""; return 1 ;;
      canceled) echo ""; return 0 ;;
      manual)  return 2 ;;  # needs manual action
    esac
    sleep 5
  done
}

wait_for_pipeline_manual() {
  local pipeline_id=$1 timeout=${2:-300}
  local start_time
  start_time=$(date +%s)
  while true; do
    local elapsed=$(( $(date +%s) - start_time ))
    if [ "$elapsed" -ge "$timeout" ]; then
      warn "Pipeline #$pipeline_id did not reach manual state"
      return 1
    fi
    local status
    status=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id" | jq -r '.status')
    printf "\r  Pipeline #%s: %-20s (%ds)" "$pipeline_id" "$status" "$elapsed"
    if [ "$status" = "manual" ]; then
      echo ""
      return 0
    fi
    if [ "$status" = "failed" ] || [ "$status" = "success" ] || [ "$status" = "canceled" ]; then
      echo ""
      warn "Pipeline ended with status: $status"
      return 1
    fi
    sleep 5
  done
}

play_job() {
  local pipeline_id=$1 job_name=$2
  local job_id
  job_id=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id/jobs?per_page=100&scope[]=manual" | \
    jq -r ".[] | select(.name == \"$job_name\") | .id")
  if [ -z "$job_id" ] || [ "$job_id" = "null" ]; then
    die "Could not find manual job '$job_name' in pipeline #$pipeline_id"
  fi
  gitlab_api POST "/projects/$PROJECT_ID/jobs/$job_id/play" > /dev/null
  success "Played: $job_name (job #$job_id)"
}

get_job_status() {
  local pipeline_id=$1 job_name=$2
  gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id/jobs?per_page=100" | \
    jq -r ".[] | select(.name == \"$job_name\") | .status" | head -1
}

get_job_log() {
  local pipeline_id=$1 job_name=$2 lines=${3:-30}
  local job_id
  job_id=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id/jobs?per_page=100" | \
    jq -r ".[] | select(.name == \"$job_name\") | .id" | head -1)
  if [ -n "$job_id" ] && [ "$job_id" != "null" ]; then
    gitlab_api GET "/projects/$PROJECT_ID/jobs/$job_id/trace" 2>/dev/null | \
      sed 's/\x1b\[[0-9;]*m//g' | tail -"$lines" | sed 's/^/    /'
  fi
}

# ── Main ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}${CYAN}  Rollback Test — canary rollback path${NC}"
echo ""

# 1. Verify GitLab is running
detect_container_runtime
info "Checking GitLab..."
running=$($CONTAINER_RUNTIME ps --filter "name=^${CONTAINER_NAME}$" --format "{{.ID}}" 2>/dev/null || true)
if [ -z "$running" ]; then
  die "GitLab container not running. Run demo/demo.sh first to start it."
fi
success "GitLab container running"

# 2. Authenticate
info "Authenticating..."
oauth_resp=$(curl -s -X POST "${GITLAB_URL}/oauth/token" \
  -d "grant_type=password" \
  --data-urlencode "username=${GITLAB_USER}" \
  --data-urlencode "password=${GITLAB_PASSWORD}")
oauth_token=$(echo "$oauth_resp" | jq -r '.access_token // empty')
if [ -z "$oauth_token" ]; then die "OAuth login failed"; fi

expires_at=$(date -v+30d +%Y-%m-%d 2>/dev/null || date -d "+30 days" +%Y-%m-%d)
pat_resp=$(curl -s -X POST \
  -H "Authorization: Bearer $oauth_token" \
  "${GITLAB_URL}/api/v4/users/1/personal_access_tokens" \
  -d "name=rollback-test-token" \
  -d "scopes[]=api" \
  -d "scopes[]=read_user" \
  -d "scopes[]=read_repository" \
  -d "scopes[]=write_repository" \
  -d "expires_at=${expires_at}")
TOKEN_VALUE=$(echo "$pat_resp" | jq -r '.token // empty')
if [ -z "$TOKEN_VALUE" ]; then die "PAT creation failed"; fi
success "Authenticated"

# 3. Delete existing project if present
existing=$(gitlab_api GET "/projects?search=$PROJECT_NAME" 2>/dev/null | jq -r ".[0].id // empty")
if [ -n "$existing" ] && [ "$existing" != "null" ]; then
  gitlab_api DELETE "/projects/$existing" > /dev/null 2>&1 || true
  sleep 3
fi

# 4. Setup work dir and runner
WORK_DIR=$(mktemp -d /tmp/rollback-test-XXXXXX)
RUNNER_CONFIG="$WORK_DIR/runner-config.toml"

runner_resp=$(gitlab_api POST "/user/runners" \
  --data-urlencode "runner_type=instance_type" \
  --data-urlencode "description=rollback-test-runner" \
  --data-urlencode "run_untagged=true" \
  --data-urlencode "access_level=not_protected")
RUNNER_AUTH_TOKEN=$(echo "$runner_resp" | jq -r '.token')
if [ -z "$RUNNER_AUTH_TOKEN" ] || [ "$RUNNER_AUTH_TOKEN" = "null" ]; then
  die "Failed to create runner"
fi

gitlab-runner register \
  --non-interactive \
  --url "$GITLAB_URL" \
  --token "$RUNNER_AUTH_TOKEN" \
  --executor shell \
  --builds-dir "$WORK_DIR/../rollback-test-builds" \
  --clone-url "$GITLAB_URL" \
  --config "$RUNNER_CONFIG" \
  --env "PATH=$HOME/.local/bin:$HOME/.go/bin:$HOME/.go/current/bin:/usr/local/bin:/usr/bin:/bin" 2>/dev/null

gitlab-runner run --config "$RUNNER_CONFIG" > "$WORK_DIR/runner.log" 2>&1 &
RUNNER_PID=$!
success "Runner started (PID: $RUNNER_PID)"

# 5. Create project with infrastructure
info "Creating project and infrastructure..."

# Create grunter config
mkdir -p "$WORK_DIR/.grunter"
cat > "$WORK_DIR/.grunter/config.yml" << 'EOF'
deploy_branch: main
tf_binary: tofu
tg_binary: terragrunt
ignore:
  - "**/.terraform.lock.hcl"
  - "**/README.md"

environments:
  - name: dev
    path: envs/dev
    regions:
      - eu-west-1
  - name: staging
    path: envs/staging
    regions:
      - eu-west-1
  - name: production
    path: envs/production
    regions:
      - eu-west-1
      - eu-central-1
EOF

# Root terragrunt
cat > "$WORK_DIR/root.hcl" << 'TGEOF'
remote_state {
  backend = "local"
  config = {
    path = "${get_terragrunt_dir()}/terraform.tfstate"
  }
  generate = {
    path      = "backend.tf"
    if_exists = "overwrite"
  }
}
TGEOF

# Module
mkdir -p "$WORK_DIR/modules/app"
cat > "$WORK_DIR/modules/app/main.tf" << 'EOF'
variable "environment" { type = string }
variable "region" { type = string }

resource "null_resource" "app" {
  triggers = {
    name    = "myapp"
    version = "1.0"
  }
}
EOF

# Create units for each env/region
for env in dev staging production; do
  for region in eu-west-1; do
    dir="$WORK_DIR/envs/$env/$region/app"
    mkdir -p "$dir"
    cat > "$dir/terragrunt.hcl" << TGEOF
include "root" {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "../../../../modules/app"
}

inputs = {
  environment = "$env"
  region      = "$region"
}
TGEOF
  done
done

# Production has eu-central-1 too
dir="$WORK_DIR/envs/production/eu-central-1/app"
mkdir -p "$dir"
cat > "$dir/terragrunt.hcl" << 'TGEOF'
include "root" {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "../../../../modules/app"
}

inputs = {
  environment = "production"
  region      = "eu-central-1"
}
TGEOF

# Generate pipeline
(cd "$WORK_DIR" && grunter init -c .grunter/config.yml --force) > /dev/null 2>&1
success "Pipeline generated"

# Init git and push
(cd "$WORK_DIR" && \
  git init -b main && \
  git config user.email "test@example.com" && \
  git config user.name "Rollback Test" && \
  git add -A && \
  git commit -m "Initial infrastructure" \
) > /dev/null 2>&1

project_resp=$(gitlab_api POST "/projects" \
  --data-urlencode "name=$PROJECT_NAME" \
  --data-urlencode "visibility=internal" \
  --data-urlencode "initialize_with_readme=false")
PROJECT_ID=$(echo "$project_resp" | jq -r '.id')
success "Project created (ID: $PROJECT_ID)"

gitlab_api POST "/projects/$PROJECT_ID/variables" \
  -d "key=GITLAB_TOKEN&value=$TOKEN_VALUE" > /dev/null 2>&1 || true
gitlab_api POST "/projects/$PROJECT_ID/variables" \
  -d "key=GIT_DEPTH&value=0" > /dev/null 2>&1 || true
CI_SERVER_URL=$(echo "$GITLAB_URL" | sed "s|localhost|$(hostname)|")
gitlab_api POST "/projects/$PROJECT_ID/variables" \
  -d "key=CI_SERVER_URL&value=$CI_SERVER_URL" > /dev/null 2>&1 || true
success "CI variables configured"

(cd "$WORK_DIR" && \
  git remote add origin "http://root:${TOKEN_VALUE}@localhost:8929/${PROJECT_PATH}.git" && \
  git push -u origin main \
) > /dev/null 2>&1
success "Baseline pushed"

# Wait for baseline pipeline to finish (or cancel it)
sleep 5
baseline_id=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines?ref=main&per_page=1" | jq -r '.[0].id // empty')
if [ -n "$baseline_id" ] && [ "$baseline_id" != "null" ]; then
  info "Waiting for baseline pipeline #$baseline_id..."
  wait_for_pipeline_completion "$baseline_id" 300 || true
fi

# 6. Make a change and merge directly to main (to trigger post-merge pipeline)
info "Pushing change to main to trigger deployment pipeline..."
(cd "$WORK_DIR" && \
  sed -i '' 's/version = "1.0"/version = "2.0"/' modules/app/main.tf && \
  git add -A && \
  git commit -m "Update app version to 2.0" && \
  git push origin main \
) > /dev/null 2>&1
success "Change pushed to main"

# 7. Wait for post-merge pipeline
sleep 5
pipeline_id=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines?ref=main&per_page=1" | jq -r '.[0].id // empty')
if [ -z "$pipeline_id" ] || [ "$pipeline_id" = "null" ]; then
  die "No pipeline found"
fi
info "Deployment pipeline: #$pipeline_id"

# Wait for staging to deploy and generate-production to run, then approve:production-deploy gate
info "Waiting for approve:production-deploy gate..."
wait_for_pipeline_manual "$pipeline_id" 300
play_job "$pipeline_id" "approve:production-deploy"

# Wait for canary to deploy, then canary gate
info "Waiting for canary gate (approve/rollback)..."
sleep 10
wait_for_pipeline_manual "$pipeline_id" 300

# 8. THIS IS THE TEST: play rollback instead of approve
echo ""
echo -e "${BOLD}${YELLOW}  >>> Playing ROLLBACK instead of approve <<<${NC}"
echo ""
play_job "$pipeline_id" "rollback:production-canary"

# 9. Wait and check the outcome
info "Waiting for rollback to complete..."
sleep 15

# Check rollback job status
for _ in $(seq 1 60); do
  rollback_status=$(get_job_status "$pipeline_id" "rollback:production-canary")
  printf "\r  rollback:production-canary: %-15s" "$rollback_status"
  if [ "$rollback_status" = "success" ] || [ "$rollback_status" = "failed" ]; then
    echo ""
    break
  fi
  sleep 5
done

# Show rollback job log
echo ""
info "Rollback job log:"
get_job_log "$pipeline_id" "rollback:production-canary" 40

# Check that rollout-production did NOT run
rollout_status=$(get_job_status "$pipeline_id" "rollout-production")
approve_rollout_status=$(get_job_status "$pipeline_id" "approve:production-rollout")

echo ""
info "Job statuses after rollback:"
info "  rollback:production-canary  = $rollback_status"
info "  approve:production-canary   = $(get_job_status "$pipeline_id" "approve:production-canary")"
info "  approve:production-rollout  = $approve_rollout_status"
info "  rollout-production          = $rollout_status"

# Verify
echo ""
if [ "$rollback_status" = "success" ]; then
  success "PASS: Rollback job succeeded"
else
  warn "FAIL: Rollback job status: $rollback_status"
fi

# approve:production-rollout should still be manual (not played) or created/skipped
if [ "$rollout_status" = "created" ] || [ "$rollout_status" = "manual" ] || [ -z "$rollout_status" ]; then
  success "PASS: rollout-production did NOT execute (status: ${rollout_status:-not created})"
else
  warn "FAIL: rollout-production has unexpected status: $rollout_status"
fi

# Pipeline should still be in manual state (approve:production-canary is still unplayed)
final_status=$(gitlab_api GET "/projects/$PROJECT_ID/pipelines/$pipeline_id" | jq -r '.status')
info "Final pipeline status: $final_status"

echo ""
echo -e "${BOLD}${GREEN}  Rollback test complete${NC}"
echo ""
