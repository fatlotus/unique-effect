#!/bin/bash
# Copyright 2021 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


set -e

mkdir -p gen/binaries/ gen/sources/ gen/outputs/

go install github.com/fatlotus/unique_effect/...

go get github.com/kisielk/errcheck
errcheck -exclude errcheck_exclude.txt ./...

go get github.com/gordonklaus/ineffassign
ineffassign ./...

for filename in examples/*.ht; do
  if [[ "${filename}" == "examples/stdlib.ht" ]]; then
    continue
  fi

  module="$(basename "${filename}" .ht)"

  unique_effect "${module}"
  clang -g -o "gen/binaries/${module}" -fsanitize=address \
    gen/builtins.c \
    "gen/sources/${module}.c" \
     -DGENERATED_MODULE_HEADER="\"sources/${module}.h\""
  ASAN_OPTIONS=detect_leaks=0 "gen/binaries/${module}" \
    | tee "gen/outputs/${module}.txt"
  diff -U 3 "gen/outputs/${module}.txt" "examples/${module}_output.txt"
done

echo -e "\033[1;32mOK\033[0m"