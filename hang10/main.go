package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/fatlotus/hang10"
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

	result, err := hang10.Parse(os.Args[1], sources)
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
