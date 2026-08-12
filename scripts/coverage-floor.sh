#!/usr/bin/env bash
# scripts/coverage-floor.sh fails CI if any package's go test statement
# coverage drops below its recorded floor.
#
# Floors are ratcheted from real, measured coverage -- not aspirational
# targets -- each one set a deliberate buffer below the number actually
# measured on dev when it was added, so ordinary fluctuation (a new test
# added elsewhere, a trivial helper extracted) doesn't false-positive while
# a real regression (a package quietly losing its tests, a big change
# landing untested) still gets caught. See docs/backlog.md's "Test / CI
# infrastructure" entry and the codebase-hardness audit that prompted this
# script for the reasoning.
#
# internal/authority is deliberately absent from FLOORS: its meaningful
# coverage only exists against a real PostgreSQL backend (most of its
# mutation paths are `t.Skip`-guarded without one), which this unit-only
# run never provides. ci.yml's postgres-integration job checks it
# separately, against real DB-backed numbers, right after running it.
#
# Packages with no test files or no statements at all (thin main-package
# wrappers, pure data/type packages) are absent from FLOORS on purpose --
# there is nothing a coverage percentage could mean for them.
set -euo pipefail

declare -A FLOORS=(
  [github.com/DhanushSantosh/AgentComms/cmd/agent-comms-server]=5
  [github.com/DhanushSantosh/AgentComms/internal/acpclient]=60
  [github.com/DhanushSantosh/AgentComms/internal/app]=45
  [github.com/DhanushSantosh/AgentComms/internal/buildinfo]=15
  [github.com/DhanushSantosh/AgentComms/internal/claudepath]=70
  [github.com/DhanushSantosh/AgentComms/internal/claudeserve]=55
  [github.com/DhanushSantosh/AgentComms/internal/claudetail]=65
  [github.com/DhanushSantosh/AgentComms/internal/codexserve]=60
  [github.com/DhanushSantosh/AgentComms/internal/controlplane]=55
  [github.com/DhanushSantosh/AgentComms/internal/daemon]=30
  [github.com/DhanushSantosh/AgentComms/internal/doctor]=50
  [github.com/DhanushSantosh/AgentComms/internal/draftstore]=70
  [github.com/DhanushSantosh/AgentComms/internal/durablefs]=90
  [github.com/DhanushSantosh/AgentComms/internal/failure]=55
  [github.com/DhanushSantosh/AgentComms/internal/identity]=35
  [github.com/DhanushSantosh/AgentComms/internal/interactiveserve]=70
  [github.com/DhanushSantosh/AgentComms/internal/localcache]=30
  [github.com/DhanushSantosh/AgentComms/internal/mcp]=35
  [github.com/DhanushSantosh/AgentComms/internal/onboarding]=80
  [github.com/DhanushSantosh/AgentComms/internal/opencodeclient]=55
  [github.com/DhanushSantosh/AgentComms/internal/personalauthority]=55
  [github.com/DhanushSantosh/AgentComms/internal/projection]=5
  [github.com/DhanushSantosh/AgentComms/internal/projectlifecycle]=55
  [github.com/DhanushSantosh/AgentComms/internal/protocol]=60
  [github.com/DhanushSantosh/AgentComms/internal/remote]=40
  [github.com/DhanushSantosh/AgentComms/internal/runtimeinit]=60
  [github.com/DhanushSantosh/AgentComms/internal/service]=55
  [github.com/DhanushSantosh/AgentComms/internal/sessionbind]=65
  [github.com/DhanushSantosh/AgentComms/internal/store]=18
  [github.com/DhanushSantosh/AgentComms/internal/terminallaunch]=40
  [github.com/DhanushSantosh/AgentComms/internal/tui]=60
  [github.com/DhanushSantosh/AgentComms/internal/worker]=45
)

output=$(go test -cover ./... 2>&1)
echo "$output"

failed=0
while IFS= read -r line; do
  case "$line" in
    ok\ *coverage:*'% of statements') ;;
    *) continue ;;
  esac
  pkg=$(awk '{print $2}' <<<"$line")
  pct=$(grep -oE 'coverage: [0-9.]+%' <<<"$line" | grep -oE '[0-9.]+')
  floor="${FLOORS[$pkg]:-}"
  [ -z "$floor" ] && continue
  below=$(awk -v p="$pct" -v f="$floor" 'BEGIN{print (p+0 < f+0) ? 1 : 0}')
  if [ "$below" = "1" ]; then
    echo "::error::$pkg coverage ${pct}% is below its floor of ${floor}%"
    failed=1
  fi
done <<<"$output"

exit "$failed"
