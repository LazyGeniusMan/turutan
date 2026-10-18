#!/bin/sh
# Guard types (one-liners):
#   type 1 - always forbidden: cmd_forbid "<ere>" "<replacement>" "<names>"
#   type 2 - help-only: cmd_help "<ere>" "<names>"
#     blocks unless payload also has standalone --help/-h/help token.
# ~keep

cmd_forbid "npm|yarn|pnpm|bun" "vp" "npm/yarn/pnpm/bun"
cmd_forbid "npx|pnpx|bunx" "vpx" "npx/pnpx/bunx"
