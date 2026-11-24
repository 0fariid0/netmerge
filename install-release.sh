#!/usr/bin/env bash
set -euo pipefail

TARGET="0fariid0/netmerge"
URL="https://github.com/$TARGET.git"
TAG="v1.2.3"
VERSION="${TAG#v}"
SOURCE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO="${REPO:-$HOME/github-projects/netmerge}"

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

for command in git gh; do
  command -v "$command" >/dev/null || fail "Install $command and run this command again."
done
[[ -f "$SOURCE/go.mod" && -f "$SOURCE/.github/workflows/release.yml" ]] ||
  fail "The source ZIP was not extracted correctly."

if ! gh auth status --hostname github.com >/dev/null 2>&1; then
  gh auth login --hostname github.com --web --git-protocol https --scopes workflow
fi
login="$(gh api --hostname github.com user --jq .login)"
[[ "$login" == 0fariid0 ]] || fail "Switch GitHub CLI to the 0fariid0 account before publishing."
branch="$(gh api --hostname github.com "repos/$TARGET" --jq .default_branch)"
git check-ref-format --branch "$branch" >/dev/null

# Keep the global Git configuration untouched.
github_git() {
  git -c credential.https://github.com.helper= \
    -c 'credential.https://github.com.helper=!gh auth git-credential' "$@"
}

if [[ ! -d "$REPO/.git" ]]; then
  mkdir -p -- "$(dirname -- "$REPO")"
  github_git clone "$URL" "$REPO"
fi
REPO="$(cd -- "$REPO" && pwd -P)"
[[ "$REPO" != "$SOURCE" ]] || fail "Choose a checkout outside the extracted source folder."
cd -- "$REPO"
remote="$(git config --get remote.origin.url)"
case "$remote" in
  "$URL" | "${URL%.git}" | "git@github.com:$TARGET.git") ;;
  *) fail "The checkout at $REPO belongs to $remote." ;;
esac
[[ -z "$(git status --porcelain)" ]] || fail "Commit or move the local changes in $REPO first."

remote_tag="$(github_git ls-remote "$URL" "refs/tags/$TAG")"
if [[ -n "$remote_tag" ]]; then
  github_git fetch "$URL" "refs/tags/$TAG:refs/tags/$TAG"
  release_commit="$(git rev-parse "$TAG^{commit}")"
  # A version must always refer to the same source, even after a retry.
  tmp_index="$(mktemp)"
  rm -f -- "$tmp_index"
  trap 'rm -f -- "$tmp_index" "$tmp_index.lock"' EXIT
  if command -v cygpath >/dev/null; then
    index_path="$(cygpath -am "$tmp_index")"
  else
    index_path="$tmp_index"
  fi
  GIT_INDEX_FILE="$index_path" git read-tree "$release_commit"
  GIT_INDEX_FILE="$index_path" git --work-tree="$SOURCE" add --ignore-removal -- .
  GIT_INDEX_FILE="$index_path" git diff --cached --quiet "$release_commit" -- ||
    fail "$TAG already exists and points to different source."
else
  github_git fetch --no-tags "$URL" "+refs/heads/*:refs/remotes/origin/*"
  if git show-ref --verify --quiet "refs/heads/$branch"; then
    git switch "$branch"
  elif git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
    git switch --track "origin/$branch"
  elif git rev-parse --verify HEAD >/dev/null 2>&1; then
    fail "Could not identify the target branch $branch."
  else
    git symbolic-ref HEAD "refs/heads/$branch"
  fi
  if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
    git merge --ff-only "origin/$branch"
    if [[ "$(git rev-parse HEAD)" != "$(git rev-parse "origin/$branch")" ]]; then
      [[ "$(git rev-parse HEAD^)" == "$(git rev-parse "origin/$branch")" &&
        "$(git log -1 --format=%s)" == "NetMerge Windows $TAG" ]] ||
        fail "The local branch has other unpublished commits. Use a fresh checkout."
    fi
  fi

  while IFS= read -r -d '' item; do
    cp -a -- "$item" "$REPO/"
  done < <(find "$SOURCE" -mindepth 1 -maxdepth 1 ! -name .git -print0)
  # Remove the former dashboard sources from older checkouts. The tray has no GUI child.
  git rm --ignore-unmatch -- internal/dashboard/{dashboard.go,dashboard_other.go,process_windows.go,window_windows.go,window.ps1}
  git rm --ignore-unmatch -- internal/engine/loadtest.go internal/tray/diagnostics.go
  git add --all
  if ! git diff --cached --quiet; then
    account_id="$(gh api --hostname github.com user --jq .id)"
    git -c "user.name=$login" -c "user.email=$account_id+$login@users.noreply.github.com" \
      commit -m "NetMerge Windows $TAG"
  fi
  release_commit="$(git rev-parse HEAD)"
  if git show-ref --verify --quiet "refs/tags/$TAG"; then
    [[ "$(git rev-parse "$TAG^{commit}")" == "$release_commit" ]] ||
      fail "The local $TAG points to a different commit."
  else
    git tag "$TAG" "$release_commit"
  fi
  refspecs=("HEAD:refs/heads/$branch" "refs/tags/$TAG:refs/tags/$TAG")
  if push_log="$(github_git push --atomic "$URL" "${refspecs[@]}" 2>&1)"; then
    printf '%s\n' "$push_log"
  else
    printf '%s\n' "$push_log" >&2
    if ! grep -Eiq 'workflow.*(scope|permission)' <<< "$push_log"; then
      fail "Git push failed. See the error above."
    fi
    gh auth refresh --hostname github.com --scopes workflow
    github_git push --atomic "$URL" "${refspecs[@]}"
  fi
fi

printf 'GitHub Actions: https://github.com/%s/actions\n' "$TARGET"
printf 'Release: https://github.com/%s/releases/tag/%s\n' "$TARGET" "$TAG"

release_assets_ready() {
  local assets
  assets="$(gh release view "$TAG" --repo "$TARGET" --json assets --jq '[.assets[].name] | join("\n")' 2>/dev/null)" ||
    return 1
  grep -Fxq "net-merge-setup-$VERSION.exe" <<< "$assets" &&
    grep -Fxq "net-merge-$VERSION-portable.zip" <<< "$assets"
}

if release_assets_ready; then
  printf 'Release assets are already available.\n'
  exit 0
fi

for (( attempt=0; attempt<30; attempt++ )); do
  recent="$(gh run list --repo "$TARGET" --workflow release.yml --commit "$release_commit" \
    --limit 1 --json databaseId,status,conclusion \
    --jq '.[] | [.databaseId,.status,.conclusion] | @tsv' 2>/dev/null)" || recent=""
  if [[ -n "$recent" ]]; then
    IFS=$'\t' read -r run_id run_status run_conclusion <<< "$recent"
    if [[ "$run_status" == completed && "$run_conclusion" != success ]]; then
      gh run rerun "$run_id" --repo "$TARGET" --failed
    fi
    gh run watch "$run_id" --repo "$TARGET" --exit-status
    release_assets_ready || fail "Build finished but the installer and portable ZIP are not both in Releases."
    printf 'Published Windows installer and portable ZIP.\n'
    exit 0
  fi
  if (( attempt == 6 )); then
    gh workflow run release.yml --repo "$TARGET" --ref "$branch" -f "tag=$TAG"
  fi
  sleep 4
done
fail "GitHub has not started the release workflow yet. Check the Actions link above."
