#!/usr/bin/env bash
# Bump the app version in internal/version/version.go (maintainer release helper).
# Does not run during builds. Never pushes to remote.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="${ROOT}/internal/version/version.go"
VERSION_REL="internal/version/version.go"

usage() {
  cat <<'EOF'
Usage:
  ./scripts/bump-version.sh <patch|minor|major> [--commit] [--tag]
  ./scripts/bump-version.sh set <vX.Y.Z> [--commit] [--tag]

Options:
  --commit  Stage version.go and create a commit
  --tag     Create an annotated git tag (implies --commit if the bump is uncommitted)

Never pushes. After tagging, run: git push && git push --tags
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

require_git_repo() {
  git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1 \
    || die "not a git repository"
}

parse_version_line() {
  local content="$1"
  local line
  line="$(printf '%s\n' "$content" | grep -E '^\s*var Version = "' | head -n1 || true)"
  [[ -n "$line" ]] || return 1
  if [[ "$line" =~ var\ Version\ =\ \"([^\"]+)\" ]]; then
    echo "${BASH_REMATCH[1]}"
    return 0
  fi
  return 1
}

read_file_version() {
  [[ -f "$VERSION_FILE" ]] || die "missing $VERSION_FILE"
  parse_version_line "$(cat "$VERSION_FILE")" \
    || die "could not find var Version in $VERSION_FILE"
}

read_head_version() {
  if git -C "$ROOT" cat-file -e "HEAD:${VERSION_REL}" 2>/dev/null; then
    parse_version_line "$(git -C "$ROOT" show "HEAD:${VERSION_REL}")" || true
  fi
}

validate_semver() {
  local v="$1"
  [[ "$v" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || die "version must look like vX.Y.Z (got: $v)"
}

bump_semver() {
  local current="$1" kind="$2"
  validate_semver "$current"
  local bare="${current#v}"
  local major minor patch
  IFS=. read -r major minor patch <<<"$bare"
  case "$kind" in
    patch) patch=$((patch + 1)) ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    major) major=$((major + 1)); minor=0; patch=0 ;;
    *) die "invalid bump kind: $kind" ;;
  esac
  echo "v${major}.${minor}.${patch}"
}

write_version() {
  local new="$1"
  local tmp
  tmp="$(mktemp)"
  awk -v new="$new" '
    /^[[:space:]]*var Version = "/ {
      sub(/var Version = "[^"]*"/, "var Version = \"" new "\"")
    }
    { print }
  ' "$VERSION_FILE" >"$tmp"
  mv "$tmp" "$VERSION_FILE"
}

tree_dirty_excluding_version() {
  local porcelain
  porcelain="$(git -C "$ROOT" status --porcelain)"
  [[ -z "$porcelain" ]] && return 1
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    local path="${line:3}"
    if [[ "$path" == *" -> "* ]]; then
      path="${path##* -> }"
    fi
    if [[ "$path" != "$VERSION_REL" ]]; then
      return 0
    fi
  done <<<"$porcelain"
  return 1
}

DO_COMMIT=0
DO_TAG=0
KIND=""
SET_VERSION=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    --commit)
      DO_COMMIT=1
      shift
      ;;
    --tag)
      DO_TAG=1
      DO_COMMIT=1
      shift
      ;;
    patch|minor|major)
      [[ -z "$KIND" ]] || die "bump kind already set"
      KIND="$1"
      shift
      ;;
    set)
      [[ -z "$KIND" ]] || die "bump kind already set"
      KIND="set"
      shift
      [[ $# -gt 0 ]] || die "set requires a version (e.g. v1.2.3)"
      SET_VERSION="$1"
      shift
      ;;
    *)
      die "unknown argument: $1 (try --help)"
      ;;
  esac
done

[[ -n "$KIND" ]] || { usage >&2; exit 1; }

require_git_repo

FILE_VER="$(read_file_version)"
HEAD_VER="$(read_head_version)"
BASE="${HEAD_VER:-$FILE_VER}"
validate_semver "$BASE"
validate_semver "$FILE_VER"

if [[ "$KIND" == "set" ]]; then
  NEW="$SET_VERSION"
  validate_semver "$NEW"
else
  NEW="$(bump_semver "$BASE" "$KIND")"
  if [[ "$FILE_VER" != "$BASE" && "$FILE_VER" != "$NEW" ]]; then
    die "version.go is ${FILE_VER}; for ${KIND} bump expected ${BASE} or already-bumped ${NEW}"
  fi
fi

if [[ "$BASE" == "$NEW" && "$FILE_VER" == "$NEW" ]]; then
  die "version is already $NEW"
fi

if git -C "$ROOT" rev-parse -q --verify "refs/tags/${NEW}" >/dev/null 2>&1; then
  die "tag ${NEW} already exists"
fi

if [[ "$DO_COMMIT" -eq 1 ]] && tree_dirty_excluding_version; then
  die "working tree has unrelated changes; commit or stash them before --commit/--tag"
fi

if [[ "$FILE_VER" != "$NEW" ]]; then
  write_version "$NEW"
  echo "Bumped ${FILE_VER} -> ${NEW} in ${VERSION_REL}"
else
  echo "version.go already at ${NEW}"
fi

if [[ "$DO_COMMIT" -eq 0 ]]; then
  cat <<EOF

Next steps (optional):
  git add ${VERSION_REL}
  git commit -m "chore: bump version to ${NEW}"
  git tag -a ${NEW} -m "Release ${NEW}"
  git push && git push --tags

Or finish with:
  ./scripts/bump-version.sh ${KIND}${SET_VERSION:+ ${SET_VERSION}} --commit --tag
EOF
  exit 0
fi

git -C "$ROOT" add -- "$VERSION_FILE"

# Nothing to commit if HEAD already has NEW (tag-only path).
if git -C "$ROOT" diff --cached --quiet -- "$VERSION_FILE"; then
  if [[ "$(read_head_version)" != "$NEW" ]]; then
    die "staged ${VERSION_REL} does not change version to ${NEW}; resolve manually"
  fi
  echo "version.go already committed at ${NEW}"
else
  git -C "$ROOT" commit -m "chore: bump version to ${NEW}"
  echo "Created commit: chore: bump version to ${NEW}"
fi

if [[ "$DO_TAG" -eq 1 ]]; then
  if git -C "$ROOT" rev-parse -q --verify "refs/tags/${NEW}" >/dev/null 2>&1; then
    die "tag ${NEW} already exists"
  fi
  git -C "$ROOT" tag -a "${NEW}" -m "Release ${NEW}"
  echo "Created annotated tag ${NEW}"
fi

cat <<EOF

Done. Push when ready:
  git push && git push --tags
EOF
