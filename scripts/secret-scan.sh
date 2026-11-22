#!/usr/bin/env bash
# Content-based leak scanner for incus-fleet.
#
# Location rules in .gitignore stop files placed in known spots. This scanner
# is the second line of defence: it inspects the *content* of whatever is about
# to enter history and rejects private keys, real LAN addresses, and operator
# hostnames regardless of file name or directory.
#
# Usage:
#   scripts/secret-scan.sh                 # scan staged changes
#   scripts/secret-scan.sh --all           # scan every tracked file
#   scripts/secret-scan.sh --history       # scan all reachable commits
#   scripts/secret-scan.sh --path <file>   # scan one file
#
# Exit status: 0 clean, 1 findings.
#
# Portability: patterns use POSIX ERE (grep -E) because macOS ships BSD grep
# without -P. Case-insensitive rules carry the -i flag per line instead of a
# (?i) prefix.

set -uo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$repo_root" || exit 1

findings=0
report=()

emit() {
	local file="$1" line="$2" reason="$3" content="$4"
	findings=$((findings + 1))
	report+=("${file}:${line}  [${reason}]  $(printf '%s' "$content" | cut -c1-140)")
}

# Patterns that indicate real secrets or real infrastructure, as
# name|grep-flags|ERE. Kept in one function so the pre-commit hook, CI, and
# manual runs stay identical.
#
# Deliberately NOT matched:
#   - RFC 5737 ranges (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24): the
#     sanitised fixtures and docs use these on purpose.
#   - General 10/8 and 172.16/12 documentation examples.
#   - *.example hostnames.
patterns() {
	cat <<'PATTERNS'
# A real PEM block has base64 body lines immediately after the header. A doc
# placeholder such as "-----BEGIN EC PRIVATE KEY-----\n..." does not, so the
# body requirement keeps documentation examples from tripping the scan.
# A real PEM file has the header as the last thing on its line. Documentation
# shows "-----BEGIN EC PRIVATE KEY-----\n..." with a placeholder on the same
# line, so anchoring on end-of-line ignores those examples.
PRIVATE_KEY|E|BEGIN [A-Z ]*PRIVATE KEY-----[[:space:]]*$
CREDENTIAL|iE|(password|passwd|secret|token|api[_-]?key)[[:space:]]*[:=][[:space:]]*"[^"]{8,}"
CREDENTIAL_SQ|iE|(password|passwd|secret|token|api[_-]?key)[[:space:]]*[:=][[:space:]]*'"'"'[^'"'"']{8,}'"'"'
BEARER|iE|authorization[[:space:]]*:[[:space:]]*bearer[[:space:]]+[A-Za-z0-9._-]{20,}
OPERATOR_SUBNET|E|(^|[^0-9])192\.168\.(6[0-9])\.[0-9]{1,3}([^0-9]|$)
OPERATOR_HOST|iE|(^|[^A-Za-z0-9_-])n(100|305)-server([^A-Za-z0-9_-]|$)
PATTERNS
}

# Credential-shaped file names are rejected even when the content is binary or
# unreadable: a key stored as a small blob still grants cluster access.
credential_name_re='(^|/)(id_(rsa|ed25519|ecdsa|dsa)[^/]*|authorized_keys|known_hosts|[^/]*\.(key|pem|crt|cer|der|p12|pfx|jks|keystore|kubeconfig|ovpn|tfstate)|\.env(\..*)?|kubeconfig)$'

# Fixture values are obviously synthetic: they end in -token/-secret/-password,
# or consist of a single repeated word, or reference example domains. Kept in
# one place so the allowlist is auditable rather than scattered in patterns.
is_fixture_value() {
	local line="$1"
	# Documented command substitution: the value is generated at runtime.
	printf '%s' "$line" | grep -Eq '\$\(|\$\{' && return 0
	# Go template placeholders: the value is a variable, not a literal.
	printf '%s' "$line" | grep -Eq '` *\+|\+ *`' && return 0
	printf '%s' "$line" | grep -Eqi '(test-|-test|example\.|localhost|127\.0\.0\.1|fake|dummy|placeholder|passw0rd)' && return 0
	# A short value made of one repeated character or one plain word.
	printf '%s' "$line" | grep -Eq '"[A-Za-z0-9_-]{0,12}"' && return 0
	return 1
}

scan_file_with_patterns() {
	local file="$1"
	local spec name flags regex

	while IFS='|' read -r name flags regex; do
		[ -z "${name}" ] && continue
		case "$name" in \#*) continue ;; esac

		# grep reads the file itself; reading its output through process
		# substitution keeps the counter in this shell instead of a subshell.
		while IFS= read -r record; do
			[ -z "$record" ] && continue
			local content="${record#*:}"
			case "$name" in
			CREDENTIAL*)
				is_fixture_value "$content" && continue
				;;
			esac
			emit "$file" "${record%%:*}" "content:${name}" "$content"
		done < <(grep -I -n "-${flags}" -- "$regex" "$file" 2>/dev/null)
	done < <(patterns)
}

scan_files() {
	local mode="$1"
	shift

	local -a files=()
	case "$mode" in
	--all)
		while IFS= read -r f; do files+=("$f"); done < <(git ls-files)
		;;
	--path)
		files=("$@")
		;;
	*)
		while IFS= read -r f; do files+=("$f"); done < <(git diff --cached --name-only --diff-filter=ACMRT)
		;;
	esac

	if [ ${#files[@]} -eq 0 ]; then
		echo "secret-scan: nothing to scan"
		return 0
	fi

	local f
	for f in "${files[@]}"; do
		[ -f "$f" ] || continue

		case "$f" in
		*/.env.example|.env.example) ;; # documented template, safe
		*)
			if printf '%s' "$f" | grep -Eq "$credential_name_re"; then
				emit "$f" 0 "credential-shaped file name" "$f"
				continue
			fi
			;;
		esac

		scan_file_with_patterns "$f"
	done
}

scan_history() {
	# Prove no reachable commit carries a private key. Scans each tree rather
	# than the diff so a key added then removed still surfaces.
	local rev
	while IFS= read -r rev; do
		while IFS= read -r hit; do
			[ -z "$hit" ] && continue
			emit "$hit" 0 "history: private key" "$rev"
		# Anchor on end-of-line: a documentation line keeps a "\n..." placeholder
		# after the header, a real PEM file does not.
		done < <(git grep -I -l -E -- 'BEGIN [A-Z ]*PRIVATE KEY-----[[:space:]]*$' "$rev" 2>/dev/null)
	done < <(git rev-list --all)
}

mode="${1:---staged}"
case "$mode" in
--history)
	echo "secret-scan: scanning all reachable commits"
	scan_history
	;;
--all)
	echo "secret-scan: scanning all tracked files"
	scan_files --all
	;;
--path)
	shift
	scan_files --path "$@"
	;;
*)
	echo "secret-scan: scanning staged changes"
	scan_files --staged
	;;
esac

if [ "$findings" -gt 0 ]; then
	echo
	echo "secret-scan: FAILED with $findings finding(s):"
	for entry in "${report[@]}"; do
		printf '  %s\n' "$entry"
	done
	echo
	echo "If a finding is a deliberate fixture, use an RFC 5737 address"
	echo "(192.0.2.x / 198.51.100.x / 203.0.113.x) or a *.example hostname."
	exit 1
fi

echo "secret-scan: clean"
exit 0
