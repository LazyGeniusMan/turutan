#!/bin/sh
# bash-guard.sh - thin loader (mirrors .config/hk.pkl).
# Usage: printf '{"tool_input":{"cmd":"npm install"}}' | sh bash-guard.sh
# Reads entire stdin as JSON payload {"tool_input":{...}}, scans whole payload.
# Exit 0 silent = allow; exit non-zero with message on stdout = block.
# Rules live in bash-guard/*.sh, sourced via bash-guard/index.sh.
# Guard types (one-liners):
#   type 1 - always forbidden: cmd_forbid "<ere>" "<replacement>" "<names>"
#   type 2 - help-only: cmd_help "<ere>" "<names>"
#     blocks unless payload also has standalone --help/-h/help token.
# ~keep
# shellcheck disable=SC2329,SC1091 # cmd_* are invoked by sourced rule files; index.sh resolves dynamically at runtime

raw_payload=$(cat)
[ "$raw_payload" = "" ] && exit 0
normalized_payload=$(printf '%s' "$raw_payload" | sed 's/\\[bfnrt]/ /g')
dequoted_payload=$(printf '%s' "$normalized_payload" | sed 's/["'"'"'\\]//g')

cmd_forbid() {
	forbidden_pattern="$1"
	replacement_command="$2"
	forbidden_names="$3"
	token_start="(^|[^A-Za-z0-9_-])"
	token_end="([^A-Za-z0-9_-]|$)"
	windows_suffix="(\\.(exe|cmd))?"
	version_suffix="([-_.]?v?[0-9]+(\\.[0-9]+)*)?"
	if printf '%s\n%s' "$normalized_payload" "$dequoted_payload" | grep -E -q "$token_start($forbidden_pattern)$windows_suffix$version_suffix$token_end"; then
		printf "Blocked: use '%s' instead of '%s' (forbidden command found).\n" "$replacement_command" "$forbidden_names"
		exit 2
	fi
}

cmd_help() {
	restricted_pattern="$1"
	restricted_names="$2"
	token_start="(^|[^A-Za-z0-9_-])"
	token_end="([^A-Za-z0-9_-]|$)"
	windows_suffix="(\\.(exe|cmd))?"
	version_suffix="([-_.]?v?[0-9]+(\\.[0-9]+)*)?"
	help_option="(--help|-h|help)"
	if printf '%s\n%s' "$normalized_payload" "$dequoted_payload" | grep -E -q "$token_start($restricted_pattern)$windows_suffix$version_suffix$token_end"; then
		if printf '%s\n%s' "$normalized_payload" "$dequoted_payload" | grep -E -q "$token_start$help_option$token_end"; then
			return 0
		fi
		printf "Blocked: '%s' must be run manually by the user. Only '--help'/'-h'/'help' allowed for learning the command user need to be run.\n" "$restricted_names"
		exit 2
	fi
}

_guard_dir=$(dirname "$0")
. "$_guard_dir/bash-guards/index.sh"
unset _guard_dir

exit 0
