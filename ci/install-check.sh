#!/bin/bash
# CI helper: install the built .deb into a clean container of its target
# release and verify it resolves dependencies and loads its libraries.
# Usage inside container: bash /ci/install-check.sh /debs/<file>.deb
set -euo pipefail

deb=$1

apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends "$deb"

echo "--- dpkg ---"
dpkg-query -W -f='${Status} ${Version}\n' ibus-bamboo
echo "--- engine binary ---"
test -x /usr/lib/ibus-bamboo/ibus-engine-bamboo
echo "--- ldd ---"
ldd /usr/lib/ibus-bamboo/ibus-engine-bamboo | grep 'not found' && exit 1 || true
echo "INSTALL_OK"
