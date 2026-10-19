#!/usr/bin/env bash
set -euo pipefail

version="1.14.3"
checksum="e2bdf179c15a3652955dc44867e33ca0965c9c9b91b34267dcc5a5d639a5feee"
directory=$(mktemp -d "${RUNNER_TEMP:?}/gosub-singbox-check.XXXXXX")
archive="${directory}/sing-box.tar.gz"
curl --fail --location --silent --show-error --retry 3 \
  "https://github.com/SagerNet/sing-box/releases/download/v${version}/sing-box-${version}-linux-amd64.tar.gz" \
  --output "${archive}"
printf '%s  %s\n' "${checksum}" "${archive}" | sha256sum --check
tar --extract --gzip --file "${archive}" --directory "${directory}" \
  --no-same-owner --no-overwrite-dir -- "sing-box-${version}-linux-amd64/sing-box"
printf 'SINGBOX_BINARY=%s\n' "${directory}/sing-box-${version}-linux-amd64/sing-box" >> "${GITHUB_ENV:?}"
