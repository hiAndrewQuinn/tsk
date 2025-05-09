#!/usr/bin/env bash
set -euo pipefail

# Paths
TSV="example-sentences.tsv"
DB="example-sentences.sqlite"

# 1) Remove any old database
if [[ -f "$DB" ]]; then
  echo "Removing existing $DB…"
  rm "$DB"
fi

# 2) Create new DB, FTS5 table, and import TSV
echo "Building new FTS5 database at $DB from $TSV…"
sqlite3 "$DB" <<SQL
-- use tab as column separator for import
.separator "\t"

-- create the full-text search virtual table
CREATE VIRTUAL TABLE sentences USING fts5(
  finnish,
  english,
  tokenize = "unicode61 remove_diacritics 0"
);

-- import the TSV (will use the current separator)
.import $TSV sentences

-- optional: verify import by counting rows
SELECT 'Imported rows:' || count(*) FROM sentences;
SQL

echo "Done. Your FTS5 database is ready in $DB."
