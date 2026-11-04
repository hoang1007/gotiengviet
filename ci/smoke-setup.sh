#!/bin/bash
# CI helper (root): install the .deb plus headless deps, create a normal user,
# then run the engine smoke test as that user under Xvfb + a session bus.
# Usage inside container: bash /ci/smoke-setup.sh /debs/<file>.deb
set -euo pipefail

deb=$1

apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
  "$deb" xvfb dbus-x11 procps

id tester >/dev/null 2>&1 || useradd -m -s /bin/bash tester
install -d -o tester -g tester /tmp/gotv-runtime
chmod 700 /tmp/gotv-runtime

echo "--- dpkg ---"
dpkg-query -W -f='${Status} ${Version}\n' ibus-bamboo
echo "--- smoke ---"
su - tester -c 'XDG_RUNTIME_DIR=/tmp/gotv-runtime DISPLAY=:99 dbus-run-session -- bash /ci/smoke-user.sh'
