#!/usr/bin/env bash
#
# check-signoff_test.sh — the sign-off gate's own suite.
#
# check-signoff.sh judges git history, so nothing in the Go suites reaches it,
# and it can regress silently in both directions: a gate that stops failing
# reports a certification nobody made, and one that starts failing wedges
# every pull request.
#
# Each case builds a throwaway repository under a temp directory and runs the
# real script against it. Nothing here touches the repository it lives in.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK="$SCRIPT_DIR/check-signoff.sh"

TMPROOT="$(mktemp -d)"
trap 'rm -rf "$TMPROOT"' EXIT

pass=0
fail=0

# An identity and a date that never depend on the machine's git config or
# clock. GIT_CONFIG_GLOBAL and GIT_CONFIG_NOSYSTEM keep a contributor's own
# settings (a commit template, a default branch name) out of these repos.
git_env() {
	export GIT_AUTHOR_NAME='Test Author' GIT_AUTHOR_EMAIL='author@example.com'
	export GIT_COMMITTER_NAME='Test Author' GIT_COMMITTER_EMAIL='author@example.com'
	export GIT_AUTHOR_DATE='2026-01-01T00:00:00Z' GIT_COMMITTER_DATE='2026-01-01T00:00:00Z'
	export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
}

newrepo() { # <name>
	local d="$TMPROOT/$1"
	mkdir -p "$d"
	git_env
	git -C "$d" init -q -b main
	echo seed >"$d/f"
	git -C "$d" add f
	git -C "$d" commit -qm "build: seed

Signed-off-by: Test Author <author@example.com>"
	echo "$d"
}

commit() { # <repo> <message>
	git_env
	echo "$RANDOM$2" >>"$1/f"
	git -C "$1" add f
	git -C "$1" commit -qm "$2"
}

check() { # <name> <want:pass|fail> <repo> [args...]
	local name="$1" want="$2" repo="$3"
	shift 3
	local out status
	out="$(cd "$repo" && "$CHECK" "$@" 2>&1)" && status=0 || status=$?
	local got=pass
	[ "$status" -eq 0 ] || got=fail
	if [ "$got" = "$want" ]; then
		pass=$((pass + 1))
		printf 'ok   %s\n' "$name"
	else
		fail=$((fail + 1))
		printf 'FAIL %s (wanted %s, got %s, exit %s)\n%s\n' "$name" "$want" "$got" "$status" "$out"
	fi
}

# --- the two directions the gate exists for -------------------------------

r="$(newrepo signed)"
commit "$r" "feat(api): signed

Signed-off-by: Test Author <author@example.com>"
check "a signed commit passes" pass "$r" HEAD~1 HEAD

r="$(newrepo unsigned)"
commit "$r" "feat(api): unsigned"
check "an unsigned commit fails" fail "$r" HEAD~1 HEAD

r="$(newrepo onebad)"
commit "$r" "feat(api): signed

Signed-off-by: Test Author <author@example.com>"
commit "$r" "feat(api): unsigned, in the middle"
commit "$r" "feat(api): signed again

Signed-off-by: Test Author <author@example.com>"
check "one unsigned commit among signed ones fails" fail "$r" HEAD~3 HEAD

r="$(newrepo noaddress)"
commit "$r" "feat(api): a trailer with no address

Signed-off-by: Test Author"
check "a trailer without an email address fails" fail "$r" HEAD~1 HEAD

# --- what reading the raw message could not tell apart ---------------------

r="$(newrepo buried)"
commit "$r" "fix(controller): quote a trailer in prose

Reverting the commit whose message read:

Signed-off-by: Someone Else <else@example.com>

...which is why this change is needed."
check "a sign-off quoted in prose fails (not a trailer block)" fail "$r" HEAD~1 HEAD

# `git interpret-trailers --parse` reads the `---` as a patch divider and
# finds no trailers at all in this shape.
r="$(newrepo dependabot)"
commit "$r" "build(deps): bump example from 1.0.0 to 1.0.1

Bumps example from 1.0.0 to 1.0.1.

---
updated-dependencies:
- dependency-name: example
  dependency-type: direct:production
...

Signed-off-by: dependabot[bot] <support@github.com>
Co-authored-by: dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>"
check "a Dependabot-shaped ---/... message passes" pass "$r" HEAD~1 HEAD

# --- the properties the script documents ----------------------------------

r="$(newrepo relayed)"
git_env
echo relayed >>"$r/f"
git -C "$r" add f
GIT_AUTHOR_NAME='Original Author' GIT_AUTHOR_EMAIL='original@example.com' \
	git -C "$r" commit -qm "feat(api): a relayed patch

Signed-off-by: Original Author <original@example.com>
Signed-off-by: Relaying Maintainer <relay@example.com>"
check "author != signer passes (DCO clauses (b) and (c))" pass "$r" HEAD~1 HEAD

r="$(newrepo merges)"
base="$(git -C "$r" rev-parse HEAD)"
git -C "$r" checkout -q -b side
git_env
echo side >"$r/side.txt"
git -C "$r" add side.txt
git -C "$r" commit -qm "feat(api): on a side branch

Signed-off-by: Test Author <author@example.com>"
git -C "$r" checkout -q main
commit "$r" "feat(api): on main

Signed-off-by: Test Author <author@example.com>"
git_env
git -C "$r" merge -q --no-ff -m "Merge branch 'side'" side
check "an unsigned merge commit is exempt" pass "$r" "$base" HEAD

# --- the range is the branch's own commits ---------------------------------

r="$(newrepo basemoved)"
git -C "$r" checkout -q -b feature
commit "$r" "feat(api): my own signed work

Signed-off-by: Test Author <author@example.com>"
git -C "$r" checkout -q main
commit "$r" "feat(api): the base branch moved on, unsigned"
git -C "$r" checkout -q feature
check "commits the base added afterwards are not judged" pass "$r" main HEAD

# The remedy a failure prints must be safe to run: an ANCESTOR of the head.
# The base must diverge, or the base ref is an ancestor anyway and this
# asserts nothing.
r="$(newrepo remedy)"
git -C "$r" checkout -q -b feature
commit "$r" "feat(api): unsigned, to force the remedy to print"
git -C "$r" checkout -q main
commit "$r" "feat(api): a commit only the base branch has"
git -C "$r" checkout -q feature
remedy="$({ cd "$r" && "$CHECK" main HEAD 2>&1 || true; } | sed -n 's/.*git rebase --signoff //p')"
if [ -n "$remedy" ] && git -C "$r" merge-base --is-ancestor "$remedy" HEAD; then
	pass=$((pass + 1))
	echo "ok   the printed rebase remedy names an ancestor of HEAD"
else
	fail=$((fail + 1))
	echo "FAIL the rebase remedy must name an ancestor of HEAD, got '$remedy'"
fi

# With no argument the base ladder picks a local main when there is no remote.
r="$(newrepo ladder)"
git -C "$r" checkout -q -b feature
commit "$r" "feat(api): unsigned on a feature branch"
check "with no arguments, local main is the base" fail "$r"

# --- refusals: a skip is not a pass ----------------------------------------

r="$(newrepo zeros)"
check "an all-zeros base is refused" fail "$r" \
	0000000000000000000000000000000000000000 HEAD

r="$(newrepo nobase)"
git -C "$r" branch -q -m trunk
check "no base branch at all is refused, not passed" fail "$r"

mkdir -p "$TMPROOT/norepo"
check "outside a git repository it refuses" fail "$TMPROOT/norepo"

# --- the reporting path must not page --------------------------------------

# There is no terminal here, so this asserts the flag rather than the hang:
# every git invocation that can write to a terminal passes --no-pager.
if grep -nE '^[^#]*git ([^|]*[^-])?show|^[^#]*git log' "$CHECK" |
	grep -v -- '--no-pager' | grep -q .; then
	fail=$((fail + 1))
	echo "FAIL every git invocation that can reach a terminal must use --no-pager"
else
	pass=$((pass + 1))
	echo "ok   the reporting path cannot invoke a pager"
fi

# --- the success line names what was judged ---------------------------------

r="$(newrepo counted)"
commit "$r" "feat(api): one

Signed-off-by: Test Author <author@example.com>"
commit "$r" "feat(api): two

Signed-off-by: Test Author <author@example.com>"
if (cd "$r" && "$CHECK" HEAD~2 HEAD) | grep -q '2 commit(s)'; then
	pass=$((pass + 1))
	echo "ok   a pass says how many commits it judged"
else
	fail=$((fail + 1))
	echo "FAIL a pass must say how many commits it judged, so a wrong base is visible"
fi

echo
echo "check-signoff_test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
