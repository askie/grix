#!/usr/bin/env bash
set -euo pipefail

REAL_XCRUN="${AIBOT_REAL_XCRUN:-/usr/bin/xcrun}"

args=("$@")
xcodebuild_index="-1"
for ((i = 0; i < ${#args[@]}; i++)); do
  if [[ "${args[i]}" == "xcodebuild" ]]; then
    xcodebuild_index="${i}"
    break
  fi
done

if ((xcodebuild_index >= 0)) && [[ -n "${AIBOT_IOS_DERIVED_DATA_PATH:-}" ]]; then
  has_scheme="0"
  for arg in "${args[@]:xcodebuild_index+1}"; do
    if [[ "${arg}" == "-derivedDataPath" ]]; then
      exec "${REAL_XCRUN}" "${args[@]}"
    fi
    if [[ "${arg}" == "-scheme" ]]; then
      has_scheme="1"
    fi
  done

  # `xcodebuild -list` / `-version` reject -derivedDataPath without a scheme.
  # Flutter's settings query and archive both carry -scheme and are the calls
  # whose build products need the stable cache. Export needs no compile cache.
  if [[ "${has_scheme}" == "1" ]]; then
    exec "${REAL_XCRUN}" \
      "${args[@]:0:xcodebuild_index+1}" \
      -derivedDataPath "${AIBOT_IOS_DERIVED_DATA_PATH}" \
      "${args[@]:xcodebuild_index+1}"
  fi
fi

exec "${REAL_XCRUN}" "${args[@]}"
