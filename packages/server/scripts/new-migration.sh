#!/usr/bin/env bash
# Create a new migration with the correct next version number.
# Usage: ./scripts/new-migration.sh <slug>
#   slug: lowercase snake_case, e.g. add_user_preferences_index
set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: $0 <slug>" >&2
  echo "  slug must be lowercase snake_case, e.g. add_user_preferences_index" >&2
  exit 2
fi

slug="$1"

if ! [[ "$slug" =~ ^[a-z0-9_]+$ ]]; then
  echo "error: slug must match [a-z0-9_]+ (got: $slug)" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
migrations_dir="$script_dir/../migrations"

if [ ! -d "$migrations_dir" ]; then
  echo "error: migrations dir not found at $migrations_dir" >&2
  exit 1
fi

# Find highest existing version; default to 000 if dir is empty.
# Uses shell globbing + basename so it works on macOS/BSD find too
# (which does not support GNU's `-printf`).
highest="000"
shopt -s nullglob
for file in "$migrations_dir"/*.up.sql; do
  name="$(basename "$file")"
  if [[ "$name" =~ ^([0-9]{3})_ ]]; then
    version="${BASH_REMATCH[1]}"
    if [ "$((10#$version))" -gt "$((10#$highest))" ]; then
      highest="$version"
    fi
  fi
done
shopt -u nullglob

# 10# forces base-10 so leading zeros aren't parsed as octal.
next=$(printf '%03d' $((10#$highest + 1)))

up="$migrations_dir/${next}_${slug}.up.sql"
down="$migrations_dir/${next}_${slug}.down.sql"

if [ -e "$up" ] || [ -e "$down" ]; then
  echo "error: $up or $down already exists" >&2
  exit 1
fi

cat > "$up" <<EOF
-- ${next}: ${slug}

EOF

cat > "$down" <<EOF
-- Revert ${next}: ${slug}

EOF

echo "created $up"
echo "created $down"
