// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/fatlotus/unique_effect"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %s [module name]\n", os.Args[0])
		os.Exit(1)
	}

	files, err := ioutil.ReadDir("examples/")
	if err != nil {
		fmt.Printf("Failed to read test dir: %v\n", err)
		os.Exit(1)
		return
	}

	sources := map[string]string{}
	for _, file := range files {
		contents, err := ioutil.ReadFile("examples/" + file.Name())
		if err != nil {
			fmt.Printf("Failed to read file: %v\n", err)
			os.Exit(1)
			return
		}
		sources[file.Name()] = string(contents)
	}

	result, err := unique_effect.Parse(os.Args[1], sources)
	if err == nil {
		for name, contents := range result {
			err := ioutil.WriteFile(fmt.Sprintf("gen/sources/%s", name), []byte(contents), 0777)
			if err != nil {
				fmt.Printf("failed to write file: %s\n", err)
				os.Exit(1)
			}
		}
	} else {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
