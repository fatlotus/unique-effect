# UniqueEffect: Automatic parallelism with side effects

![CI status](https://github.com/fatlotus/unique-effect/workflows/BuildAndTest/badge.svg)

Features of UniqueEffect:
 
 *  Pure functional semantics:
 
    	let a = "Hello"
    	let b = a + " World"

 *  Uniquely typed variables:
 
    	destroy(b)
    	destroy(b) // Error! Cannot consume "b" twice.

 *  Syntactic sugar to simulate mutations. (To "mutate" an object, a function
    can modify an argument and return it back.) 

    	concat(&b, "!")

 *  Side effects are tracked using unique objects. (To print to the console, use
    the `Stream` called "`stdout`")
 
    	set stdout = print(stdout, b) // or: "print(&stdout, b)"

 *  When they do not use the same variables, operations occur in parallel.

    	print(&console, "Before")
    	sleep(&clock, 1)
    	print(&console, "After") // prints immediately after "Before"

 *  When variables do overlap, the standard library has constructs that enable
    splitting and merging effect variables.

    	let a, b = fork(clock)
    	sleep(&a, 2)
    	sleep(&b, 3)
    	let clock = join(a, b) // takes three seconds to complete

There are more examples in the `examples` directory. Each one has a
corresponding `_output.txt` file that is checked by continuous integration.

## Installing

To build the compiler and run the tests locally, please install Clang and Go,
and then run `./build_and_test.sh`.

## License and reuse

This code is covered under the Apache 2.0 License. See LICENSE for details.

I work for Google, but this is not an officially supported Google product.
