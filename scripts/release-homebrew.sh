#!/usr/bin/env bash
# Prepare a Homebrew formula update for an already-published Documax tag.
#
# The script calculates the immutable source archive's SHA-256, validates the
# local formula, then commits and pushes the Homebrew tap. Source-repository
# commits, pushes, and tag creation stay manual.
set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source_root="$(cd "$script_directory/.." && pwd)"
cd "$source_root"

usage() {
  cat <<'EOF'
Usage: scripts/release-homebrew.sh vX.Y.Z --github-user USERNAME --yes

Environment:
  HOMEBREW_TAP      Homebrew tap name (default: USERNAME/tap)
  HOMEBREW_TAP_DIR  Local tap directory (default: resolved with brew)

The source tag must already be pushed to GitHub. The source repository and
Homebrew tap must both have clean working trees.
EOF
}

version="${1:-}"
shift || true
github_user=""
confirmation=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --github-user)
      github_user="${2:-}"
      shift 2
      ;;
    --yes)
      confirmation="--yes"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$ ]] || [[ -z "$github_user" ]] || [[ "$confirmation" != "--yes" ]]; then
  usage >&2
  exit 2
fi

for command in brew curl git make ruby shasum; do
  command -v "$command" >/dev/null || {
    echo "Required command not found: $command" >&2
    exit 1
  }
done

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "Release from the main branch only." >&2
  exit 1
fi
if [[ -n "$(git status --porcelain)" ]]; then
  echo "The Documax working tree is not clean." >&2
  exit 1
fi
if ! git rev-parse -q --verify "refs/tags/$version" >/dev/null; then
  echo "Tag $version does not exist locally. Create and push it before running this script." >&2
  exit 1
fi

tap_name="${HOMEBREW_TAP:-$github_user/tap}"
tap_dir="${HOMEBREW_TAP_DIR:-$(brew --repository "$tap_name")}"
formula="$tap_dir/Formula/documax.rb"
if [[ ! -f "$formula" ]]; then
  echo "Formula not found: $formula" >&2
  exit 1
fi
if [[ -n "$(git -C "$tap_dir" status --porcelain)" ]]; then
  echo "The Homebrew tap working tree is not clean: $tap_dir" >&2
  exit 1
fi

origin="$(git remote get-url origin)"
case "$origin" in
  git@github.com:*) github_repository="${origin#git@github.com:}" ;;
  https://github.com/*) github_repository="${origin#https://github.com/}" ;;
  *)
    echo "Origin must be a GitHub repository; found: $origin" >&2
    exit 1
    ;;
esac
github_repository="${github_repository%.git}"
origin_owner="${github_repository%%/*}"
repository_name="${github_repository#*/}"
if [[ "$origin_owner" != "$github_user" ]]; then
  echo "--github-user $github_user does not match the origin owner $origin_owner." >&2
  exit 1
fi
github_repository="$github_user/$repository_name"

echo "Running release checks for existing tag $version..."
make test
make test-race

archive_url="https://github.com/$github_repository/archive/refs/tags/$version.tar.gz"
temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT
archive="$temporary_directory/documax-$version.tar.gz"

echo "Downloading immutable source archive..."
curl --fail --location --retry 3 --retry-delay 2 --output "$archive" "$archive_url"
checksum="$(shasum -a 256 "$archive" | awk '{print $1}')"

ARCHIVE_URL="$archive_url" ARCHIVE_SHA256="$checksum" ruby - "$formula" <<'RUBY'
formula = ARGV.fetch(0)
url = ENV.fetch("ARCHIVE_URL")
checksum = ENV.fetch("ARCHIVE_SHA256")
contents = File.read(formula)

unless contents.scan(/^  url "[^"]+"$/).length == 1 && contents.scan(/^  sha256 "[^"]+"$/).length == 1
  abort "Expected exactly one url and one sha256 line in #{formula}"
end

contents.sub!(/^  url "[^"]+"$/, "  url \"#{url}\"")
contents.sub!(/^  sha256 "[^"]+"$/, "  sha256 \"#{checksum}\"")
File.write(formula, contents.end_with?("\n") ? contents : "#{contents}\n")
RUBY

echo "Building and validating the updated Homebrew formula..."
brew install --build-from-source "$tap_name/documax"
brew test "$tap_name/documax"
brew audit --strict --online "$tap_name/documax"

echo "Publishing the validated Homebrew formula update..."
git -C "$tap_dir" add Formula/documax.rb
git -C "$tap_dir" commit -m "Update documax to ${version#v}"
git -C "$tap_dir" push origin main

cat <<EOF

Prepared Homebrew formula update:
  Formula:  $formula
  Version:  $version
  URL:      $archive_url
  SHA-256:  $checksum

Homebrew formula update published successfully.
EOF
