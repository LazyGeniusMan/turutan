#!/bin/sh
# ~keep
# shellcheck disable=SC2154,SC1091 # _guard_dir is set by the sourcing loader; rule files resolve dynamically at runtime

. "$_guard_dir/bash-guards/ai.sh"
. "$_guard_dir/bash-guards/custom.sh"
. "$_guard_dir/bash-guards/core.sh"
. "$_guard_dir/bash-guards/github.sh"
. "$_guard_dir/bash-guards/go.sh"
. "$_guard_dir/bash-guards/pkl.sh"
. "$_guard_dir/bash-guards/toml.sh"
. "$_guard_dir/bash-guards/yml.sh"
