#!/usr/bin/env bash
# Profile the conversation-list → chat push before and after the deferral.
#
# There is no product switch. "Before" is origin/main. "After" is the branch
# that contains this script. main has no list-open trace, so prepare-before
# copies only the trace onto a local branch and does not push it:
#   frontend/lib/modules/chat/services/private_chat_open_perf_logger.dart
#   frontend/lib/modules/home/controllers/conversations_controller_actions.dart
# The actions file is a part, so the parent library also gains the one import
# those calls need. It does not gain the list-rebuild hold.
#
# The before build logs tap and [ChatOpenFrame] from the moment of the tap.
# route_first_frame, transition_settled, and first_messages_applied are marked
# only on the after build. On the before log, treat elapsed_ms < 250 as the
# push (AppRoutes.defaultPageTransitionMilliseconds). On the after log, use
# phase=transition for that same window. phase=after_settle is the deferred
# work and is outside the transition budget.
#
# Usage:
#   frontend/scripts/profile_chat_open_transition.sh prepare-before
#   DEVICE=<udid> frontend/scripts/profile_chat_open_transition.sh run before
#   DEVICE=<udid> frontend/scripts/profile_chat_open_transition.sh run after
#   frontend/scripts/profile_chat_open_transition.sh summarize before
#   frontend/scripts/profile_chat_open_transition.sh summarize after
#
# After the list is on screen, tap one conversation. Quit the profile run
# once [ChatOpenFrame] lines for that tap have printed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${FRONTEND_DIR}/.." && pwd)"
BEFORE_BRANCH="measure/chat-open-transition-before"
BEFORE_WORKTREE="${CHAT_OPEN_BEFORE_WORKTREE:-${REPO_ROOT}/../grix-chat-open-transition-before}"
BEFORE_WORKTREE="$(cd "$(dirname "$BEFORE_WORKTREE")" && pwd)/$(basename "$BEFORE_WORKTREE")"
LOG_DIR="${CHAT_OPEN_PERF_LOG_DIR:-/tmp/grix-chat-open-perf}"
DEVICE="${DEVICE:-00008110-001E0C682252801E}"
TRANSITION_MS=250

TRACE_LOGGER="frontend/lib/modules/chat/services/private_chat_open_perf_logger.dart"
TRACE_ACTIONS="frontend/lib/modules/home/controllers/conversations_controller_actions.dart"
TRACE_PARENT="frontend/lib/modules/home/controllers/conversations_controller.dart"

usage() {
  sed -n '2,27p' "$0" | sed 's/^# \{0,1\}//'
}

refuse_push() {
  if git -C "$BEFORE_WORKTREE" rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1; then
    echo "refusing: ${BEFORE_BRANCH} tracks a remote. This baseline must stay local." >&2
    exit 1
  fi
}

prepare_before() {
  local fix_commit
  fix_commit="$(git -C "$REPO_ROOT" rev-parse HEAD)"
  git -C "$REPO_ROOT" fetch origin main

  if [[ -e "$BEFORE_WORKTREE" ]] && ! git -C "$REPO_ROOT" worktree list --porcelain | grep -Fx "worktree ${BEFORE_WORKTREE}" >/dev/null; then
    echo "path exists and is not a worktree: ${BEFORE_WORKTREE}" >&2
    exit 1
  fi

  if git -C "$REPO_ROOT" worktree list --porcelain | grep -Fx "worktree ${BEFORE_WORKTREE}" >/dev/null; then
    echo "reusing worktree ${BEFORE_WORKTREE}"
  elif git -C "$REPO_ROOT" show-ref --verify --quiet "refs/heads/${BEFORE_BRANCH}"; then
    git -C "$REPO_ROOT" worktree add "$BEFORE_WORKTREE" "$BEFORE_BRANCH"
  else
    git -C "$REPO_ROOT" worktree add -b "$BEFORE_BRANCH" "$BEFORE_WORKTREE" origin/main
  fi

  local branch
  branch="$(git -C "$BEFORE_WORKTREE" rev-parse --abbrev-ref HEAD)"
  if [[ "$branch" != "$BEFORE_BRANCH" ]]; then
    echo "worktree is on ${branch}, expected ${BEFORE_BRANCH}" >&2
    exit 1
  fi
  # Branching from origin/main sets that remote as upstream. Drop it so a
  # plain git push cannot update main. This changes branch config, not git config.
  git -C "$BEFORE_WORKTREE" branch --unset-upstream >/dev/null 2>&1 || true
  refuse_push

  git -C "$BEFORE_WORKTREE" checkout "$fix_commit" -- "$TRACE_LOGGER" "$TRACE_ACTIONS"
  python3 - "$BEFORE_WORKTREE/$TRACE_PARENT" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
text = path.read_text()
line = "import '../../chat/services/private_chat_open_perf_logger.dart';\n"
anchor = "import '../../chat/services/chat_route_navigator.dart';\n"
if line not in text:
    if anchor not in text:
        raise SystemExit(f"missing anchor import in {path}")
    text = text.replace(anchor, anchor + line, 1)
    path.write_text(text)
PY

  git -C "$BEFORE_WORKTREE" add -- "$TRACE_LOGGER" "$TRACE_ACTIONS" "$TRACE_PARENT"
  if git -C "$BEFORE_WORKTREE" diff --cached --quiet; then
    echo "before branch already contains the list-open trace"
  else
    git -C "$BEFORE_WORKTREE" commit -m "$(cat <<'EOF'
measure(chat): add list-open frame trace on main

Local comparison baseline only. Do not push. Product behavior stays on
main; this commit only starts the conversation-list open trace.

EOF
)"
  fi
  refuse_push
  echo "before worktree: ${BEFORE_WORKTREE}"
  echo "before branch (local only): ${BEFORE_BRANCH}"
  echo "trace copied from: ${fix_commit}"
}

run_profile() {
  local which="$1"
  local app_dir log
  mkdir -p "$LOG_DIR"
  case "$which" in
    before)
      if [[ ! -d "$BEFORE_WORKTREE/frontend" ]]; then
        echo "run prepare-before first" >&2
        exit 1
      fi
      refuse_push
      app_dir="$BEFORE_WORKTREE/frontend"
      log="${LOG_DIR}/before.txt"
      ;;
    after)
      app_dir="$FRONTEND_DIR"
      log="${LOG_DIR}/after.txt"
      ;;
    *)
      echo "run expects before or after" >&2
      exit 1
      ;;
  esac

  echo "profile ${which} on device ${DEVICE}"
  echo "log: ${log}"
  echo "When the conversation list is up, tap one session, then quit this run."
  if [[ "$which" == "before" ]]; then
    echo "Transition rows: [ChatOpenFrame] with elapsed_ms < ${TRANSITION_MS}."
  else
    echo "Transition rows: [ChatOpenFrame] with phase=transition."
    echo "phase=after_settle is deferred work and is outside the 16ms budget."
  fi
  (
    cd "$app_dir"
    flutter run --profile -d "$DEVICE" 2>&1 | tee "$log"
  )
}

summarize() {
  local which="$1"
  local log="${LOG_DIR}/${which}.txt"
  if [[ ! -f "$log" ]]; then
    echo "missing log: ${log}" >&2
    exit 1
  fi
  awk -v which="$which" -v limit="$TRANSITION_MS" '
    /\[ChatOpenFrame\]/ {
      elapsed = -1
      build = -1
      phase = ""
      if (match($0, /elapsed_ms=[0-9]+/)) {
        elapsed = substr($0, RSTART + 11, RLENGTH - 11) + 0
      }
      if (match($0, /build_ms=[0-9.]+/)) {
        build = substr($0, RSTART + 9, RLENGTH - 9) + 0
      }
      if (match($0, /phase=[a-z_]+/)) {
        phase = substr($0, RSTART + 6, RLENGTH - 6)
      }
      in_window = 0
      if (which == "before" && elapsed >= 0 && elapsed < limit) in_window = 1
      if (which == "after" && phase == "transition") in_window = 1
      if (!in_window) next
      frames++
      if (build > max) max = build
      if ($0 ~ /ui_over_16ms=true/) {
        over++
        print
      }
    }
    END {
      printf "window_frames=%d ui_over_16ms=%d max_build_ms=%.2f\n", frames + 0, over + 0, max + 0
    }
  ' "$log"
}

cmd="${1:-}"
case "$cmd" in
  prepare-before) prepare_before ;;
  run) run_profile "${2:-}" ;;
  summarize) summarize "${2:-}" ;;
  *) usage; exit 2 ;;
esac
