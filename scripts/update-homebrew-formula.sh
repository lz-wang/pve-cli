#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: $0 <tag> [tap-repo-dir]" >&2
  exit 2
fi

tag="$1"
tap_dir="${2:-/Users/lzwang/projects/homebrew-tap}"
formula_dir="${tap_dir}/Formula"
formula_path="${formula_dir}/pve.rb"
release_base_url="https://github.com/lz-wang/pve-cli/releases/download/${tag}"

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi

fetch_sha256() {
  local artifact="$1"
  local sha_url="${release_base_url}/${artifact}.sha256"
  local sha_line

  sha_line="$(curl -fsSL "$sha_url")"
  awk '{ print $1 }' <<<"$sha_line"
}

darwin_amd64_sha="$(fetch_sha256 "pve-${tag}-darwin-amd64")"
darwin_arm64_sha="$(fetch_sha256 "pve-${tag}-darwin-arm64")"
linux_amd64_sha="$(fetch_sha256 "pve-${tag}-linux-amd64")"
linux_arm64_sha="$(fetch_sha256 "pve-${tag}-linux-arm64")"

for value in "$darwin_amd64_sha" "$darwin_arm64_sha" "$linux_amd64_sha" "$linux_arm64_sha"; do
  if ! [[ "$value" =~ ^[0-9a-fA-F]{64}$ ]]; then
    echo "invalid sha256 value: ${value}" >&2
    exit 1
  fi
done

mkdir -p "$formula_dir"

cat >"$formula_path" <<EOF
class Pve < Formula
  desc "Personal HomeLab Proxmox VE CLI"
  homepage "https://github.com/lz-wang/pve-cli"
  license "MIT"
  version "${tag#v}"

  on_macos do
    if Hardware::CPU.arm?
      url "${release_base_url}/pve-${tag}-darwin-arm64"
      sha256 "${darwin_arm64_sha}"
    else
      url "${release_base_url}/pve-${tag}-darwin-amd64"
      sha256 "${darwin_amd64_sha}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "${release_base_url}/pve-${tag}-linux-arm64"
      sha256 "${linux_arm64_sha}"
    else
      url "${release_base_url}/pve-${tag}-linux-amd64"
      sha256 "${linux_amd64_sha}"
    end
  end

  def install
    binary = Dir["pve-*"].first
    chmod 0755, binary
    bin.install binary => "pve"
  end

  test do
    assert_match "${tag}", shell_output("#{bin}/pve version -o json")
  end
end
EOF

echo "Updated ${formula_path}"
