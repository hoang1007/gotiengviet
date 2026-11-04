#!/bin/bash
# CI helper (normal user): register the Bamboo engines with IBus, activate the
# explicit "us" layout engine, and confirm the engine process is alive.
#
# Note: engines with <layout>default</layout> become current under bare Xvfb but
# `ibus engine` returns 1 without a desktop keyboard layout, so the gate uses
# the "us" layout engine.
set -euo pipefail

export XDG_RUNTIME_DIR=/tmp/gotv-runtime
export DISPLAY=:99
mkdir -p "$XDG_RUNTIME_DIR"
chmod 700 "$XDG_RUNTIME_DIR"

Xvfb :99 -screen 0 1024x768x24 >/tmp/xvfb.log 2>&1 &
xvfb_pid=$!
cleanup() {
  ibus exit >/dev/null 2>&1 || true
  kill "$ibus_pid" "$xvfb_pid" >/dev/null 2>&1 || true
}
trap cleanup EXIT

sleep 1
ibus-daemon -rx --panel=disable >/tmp/ibus.log 2>&1 &
ibus_pid=$!
sleep 3

count=$(ibus list-engine 2>/dev/null | grep -c 'Bamboo' || true)
echo "registered Bamboo engines: $count"
[[ "$count" -ge 1 ]] || { cat /tmp/ibus.log; exit 1; }

ibus engine Bamboo::Us
[[ "$(ibus engine)" == "Bamboo::Us" ]] || { cat /tmp/ibus.log; exit 1; }
sleep 1
pgrep -af 'ibus-engine-bamboo --ibus'

ibus engine Bamboo >/dev/null 2>&1 || true
[[ "$(ibus engine)" == "Bamboo" ]]
echo "SMOKE_OK"
