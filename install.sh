#!/bin/sh
# Install the Aria Plugins tools (Commerce MCP bridge) and register them with
# Aria Code, in one command, without Go.
#
#   curl -fsSL https://raw.githubusercontent.com/artheras/aria-plugins/main/install.sh | sh
#
# It downloads the release for your platform, checks it against the published
# SHA256SUMS, installs into a directory you own, adds an entry to Aria's
# mcp_servers.json (keeping a backup and your other servers), and runs doctor.
# It does not start a server, store credentials, or change anything else.
#
# Environment: ARIA_PLUGINS_VERSION (default: latest, e.g. v0.4.0),
#              ARIA_PLUGINS_DIR (default: ~/.local/share/aria-plugins),
#              ARIA_HOME (Aria's state directory; same rule as Aria Code).
set -eu

repo=artheras/aria-plugins
fail() { printf 'aria-plugins: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required"; }
need curl; need unzip; need uname; need mktemp

case "$(uname -s)" in Darwin) os=darwin ;; Linux) os=linux ;; *) fail "unsupported OS; on Windows download the zip from the releases page" ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) fail "unsupported CPU: $(uname -m)" ;; esac

version=${ARIA_PLUGINS_VERSION:-latest}
if [ "$version" = latest ]; then
  base=https://github.com/$repo/releases/latest/download
  tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") || fail "could not resolve the latest release"
  tag=${tag##*/}
else
  printf '%s' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || fail "ARIA_PLUGINS_VERSION must look like v0.4.0"
  tag=$version
  base=https://github.com/$repo/releases/download/$tag
fi
number=${tag#v}
asset=aria-plugins_${number}_${os}_${arch}.zip

# Aria's state directory, by the same rule Aria Code uses.
if [ -n "${ARIA_HOME:-}" ]; then aria_home=$ARIA_HOME
elif [ -d "$HOME/.arthera" ]; then aria_home=$HOME/.arthera
else aria_home=$HOME/.aria-code
fi
dir=${ARIA_PLUGINS_DIR:-$HOME/.local/share/aria-plugins}

tmp=$(mktemp -d) || fail "could not create a temporary directory"
trap 'rm -rf "$tmp"' EXIT INT TERM

printf 'Downloading %s...\n' "$asset"
curl -fsSL "$base/$asset" -o "$tmp/$asset" || fail "could not download $asset"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" || fail "could not download SHA256SUMS"

want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$tmp/SHA256SUMS")
[ -n "$want" ] || fail "$asset is not listed in SHA256SUMS"
if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}'); fi
[ "$got" = "$want" ] || fail "checksum mismatch for $asset; refusing to install"

# The helper refuses symlinked locations (macOS /var is one), so resolve them.
mkdir -p "$tmp/unpacked" "$dir" "$aria_home"
dir=$(cd "$dir" && pwd -P)
aria_home=$(cd "$aria_home" && pwd -P)
unzip -q "$tmp/$asset" -d "$tmp/unpacked" || fail "could not unpack $asset"
helper=$(find "$tmp/unpacked" -type f -name aria-plugins -perm -u+x | head -n 1)
[ -n "$helper" ] || fail "the bundle has no aria-plugins helper"

"$helper" install --archive "$tmp/$asset" --sha256 "$want" --dir "$dir" --config "$aria_home/mcp_servers.json" --replace >/dev/null \
  || fail "install failed; nothing in $aria_home was overwritten without a backup"

printf 'Checking the install...\n'
"$dir/$number/bin/aria-plugins" doctor --dir "$dir/$number" >/dev/null 2>&1 \
  && printf 'Doctor: ok\n' \
  || printf 'Doctor could not reach a Commerce server yet; that is expected until ARIA_COMMERCE_URL is set.\n'

cat <<DONE

Installed Aria Plugins $number into $dir/$number
Registered the MCP bridge in $aria_home/mcp_servers.json (an existing file is backed up first).

Next, in the shell that starts Aria:
  export ARIA_COMMERCE_URL=...   # your Commerce server
  export ARIA_MERCHANT_TOKEN=...
Then in Aria Code:
  /skills add artheras/aria-plugins --ref $tag
  /skills doctor
Restart Aria after registration, then ask: \$commerce:commerce Draft an order for 2 tees and 1 mug.
DONE
