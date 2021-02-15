#!/bin/bash

set -e

mkdir -p gen/binaries/ gen/sources/ gen/outputs/

go install github.com/fatlotus/hang10/...
errcheck -exclude errcheck_exclude.txt ./...
ineffassign ./...
for filename in examples/*.ht; do
  if [[ "${filename}" == "examples/stdlib.ht" ]]; then
    continue
  fi

  module="$(basename "${filename}" .ht)"

  hang10 "${module}"
  cc -g -o "gen/binaries/${module}" -fsanitize=address \
    gen/builtins.c \
    "gen/sources/${module}.c" \
     -DGENERATED_MODULE_HEADER="\"sources/${module}.h\""
  "gen/binaries/${module}" | tee "gen/outputs/${module}.txt"
  diff -U 3 "gen/outputs/${module}.txt" "examples/${module}_output.txt"
done

echo -e "\033[1;32mOK\033[0m"