#!/usr/bin/env bash
#
# check-signoff.sh — fail unless every non-merge commit a branch ADDS carries a
# Signed-off-by trailer.
#
# The trailer is the DCO gesture: `git commit -s` writes it, and writing it is
# what certifies the DCO file at the repository root over that commit. A
# convention that holds only while everyone remembers it has already stopped
# holding, so it is checked here, by `make check` and by CI's sign-off job.
#
# Only the commits the head adds are judged: the range is anchored on the
# MERGE BASE of base and head. That does not change which commits are judged —
# `A..B` and `merge-base(A,B)..B` select the same set — but it changes the SHA
# the failure remedy names. `git rebase --signoff origin/main` in a fork whose
# main has gone stale replays commits the contributor never wrote and restamps
# them under their sign-off; a merge base is an ancestor of the head by
# construction, so a remedy naming it cannot reach a commit the branch did not
# add.
#
# WHAT THIS CHECKS IS PRESENCE, NOT IDENTITY. The trailer is not matched
# against the commit's author: the DCO's clauses (b) and (c) cover relaying
# somebody else's work, which is exactly a commit whose author and signer
# differ, and Dependabot authors its commits as `dependabot[bot]
# <…@users.noreply.github.com>` while signing them as `dependabot[bot]
# <support@github.com>`. A matcher would reject both.
#
# Merge commits are skipped (`--no-merges`): `git merge` and GitHub's merge
# button author one with no chance to sign it, and it carries no change of its
# own to certify.
#
# Usage:
#   scripts/check-signoff.sh                  # <base branch>..HEAD
#   scripts/check-signoff.sh <base>           # <base>..HEAD
#   scripts/check-signoff.sh <base> <head>
#
set -euo pipefail

usage() {
	cat <<'USAGE'
usage: scripts/check-signoff.sh [<base> [<head>]]

Fails if any non-merge commit that <head> adds relative to its merge base with
<base> lacks a Signed-off-by trailer. <base> defaults to the first of
upstream/main, origin/main and main that exists; <head> to HEAD.
USAGE
}

case "${1-}" in
-h | --help)
	usage
	exit 0
	;;
esac

die() {
	{
		echo "check-signoff.sh: $1"
		shift
		for line in "$@"; do echo "  $line"; done
	} >&2
	exit 1
}

# First, because `git rev-parse --verify --quiet` below silences "unknown
# revision" but not "not a git repository".
git rev-parse --git-dir >/dev/null 2>&1 ||
	die "not inside a git repository, so there is no history to judge." \
		"run it from a checkout of the repository whose commits you mean."

head="${2-HEAD}"

# No base given: the BASE BRANCH, in the order a checkout is likely to have a
# fresh one. upstream/main first, the usual name of the canonical remote in a
# fork clone, whose own origin/main goes stale as soon as this repository
# moves; then origin/main; then a local main for a checkout with no remote.
#
# Not the branch's @{upstream}: for a pushed feature branch that is the same
# branch on the remote, so the range would be empty and the gate would pass
# having judged nothing.
base="${1-}"
if [ -z "$base" ]; then
	for candidate in upstream/main origin/main main; do
		if git rev-parse --verify --quiet "$candidate^{commit}" >/dev/null; then
			base="$candidate"
			break
		fi
	done
fi

# A missing base fails rather than passes: with no range to judge, a pass
# would be a claim about nothing.
[ -n "$base" ] ||
	die "no base branch to compare against — none of upstream/main, origin/main" \
		"or main resolves in this checkout, so there is no range to judge." \
		"run:  git fetch origin main" \
		"or:   scripts/check-signoff.sh <base> [<head>]"

for ref in "$base" "$head"; do
	git rev-parse --verify --quiet "$ref^{commit}" >/dev/null ||
		die "'$ref' does not name a commit in this checkout." \
			"a push event's all-zeros sentinel and an unfetched commit both land here." \
			"run:  git fetch --unshallow   (or pass refs this checkout has)"
done

fork_point="$(git merge-base "$base" "$head")"
commits="$(git rev-list --no-merges "$fork_point..$head")"

# A shallow clone answers the merge-commit question WRONGLY rather than not at
# all: git rewrites a graft boundary to have no parents, so a merge commit
# there reads as an ordinary one and `--no-merges` walks straight into it. Only
# a range that CONTAINS a parentless commit is ambiguous — a graft far below
# the fork point is harmless — so that is what is refused. CI checks out with
# fetch-depth: 0 and never gets here.
if [ "$(git rev-parse --is-shallow-repository)" = "true" ] &&
	[ -n "$(git rev-list --max-parents=0 "$fork_point..$head")" ]; then
	die "this range reaches a commit with no parents in a SHALLOW clone, and a" \
		"truncated graph cannot say whether that is a root commit (which must be" \
		"signed) or a merge whose parents were cut away (which is exempt)." \
		"run:  git fetch --unshallow"
fi

# A newline-delimited string rather than an array: macOS ships bash 3.2, where
# `set -u` makes expanding an EMPTY array an unbound-variable error — and an
# empty list is what a fully signed range produces.
missing=""
checked=0
while IFS= read -r sha; do
	[ -n "$sha" ] || continue
	checked=$((checked + 1))
	# Git's PARSED trailer block, not the raw message: a Signed-off-by line
	# quoted in prose (a pasted patch, a reverted commit's message) is not a
	# certification. The %(trailers) atom rather than `git interpret-trailers
	# --parse`, which reads the `---` in Dependabot's `updated-dependencies:`
	# block as a patch divider and finds no trailers at all.
	#
	# The regex then checks the value is a name and an address. A here-string
	# rather than a pipe into `grep -q`: grep exits at its first match, the
	# writer takes SIGPIPE, and under pipefail a signed commit would read as
	# unsigned.
	trailers="$(git --no-pager show -s --format='%(trailers:key=Signed-off-by,only,unfold)' "$sha")"
	if ! grep -qE '^Signed-off-by: .+ <[^[:space:]@]+@[^[:space:]@]+>[[:space:]]*$' <<<"$trailers"; then
		missing="$missing$sha
"
	fi
done <<<"$commits"

if [ -n "$missing" ]; then
	{
		echo "commits without a Signed-off-by trailer:"
		# --no-pager: this goes to stderr, which is a terminal under `make
		# check`, and a contributor who exports LESS without -F would see the
		# gate block at (END) inside a silent recipe.
		# shellcheck disable=SC2086 # deliberate word splitting: a list of SHAs
		git --no-pager log --no-walk --format='  %h %s' $missing
		echo
		echo "Every commit certifies the DCO — see ./DCO and CONTRIBUTING.md,"
		echo "\"Sign your work\". Sign new commits with:"
		echo "  git commit -s"
		echo "and repair the ones above, which rewrites them, with:"
		echo "  git rebase --signoff $fork_point"
	} >&2
	exit 1
fi

# Say what was judged: silence on success cannot tell "every commit is signed"
# from "the base resolved wrong and nothing was looked at".
echo "check-signoff.sh: $checked commit(s) in $fork_point..$head, all signed off"
