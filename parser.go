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

package unique_effect

import (
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
	"github.com/alecthomas/participle/v2/lexer/stateful"
)

type Family int

type Kind struct {
	Borrowed bool   `@"&"?`
	Family   Family `@Ident`
	Args     []*Kind
}

const (
	FamilyString Family = iota
	FamilyStream
	FamilyClock
	FamilyTuple
	FamilyInteger
	FamilyBoolean
)

func (f Family) String() string {
	switch f {
	case FamilyStream:
		return "Stream"
	case FamilyString:
		return "String"
	case FamilyClock:
		return "Clock"
	case FamilyInteger:
		return "Integer"
	case FamilyBoolean:
		return "Boolean"
	default:
		return "?? Unknown"
	}
}

var _ = participle.Capture(new(Family))

func (f *Family) Capture(values []string) error {
	switch values[0] {
	case "String":
		*f = FamilyString
	case "Stream":
		*f = FamilyStream
	case "Clock":
		*f = FamilyClock
	case "Integer":
		*f = FamilyInteger
	case "Boolean":
		*f = FamilyBoolean
	default:
		return fmt.Errorf("Unknown type: %s", values[0])
	}
	return nil
}

func (k Kind) String() string {
	result := ""
	if k.Borrowed {
		result += "&"
	}
	return result + k.Family.String()
}

func (k Kind) CanConvertTo(other Kind) error {
	if k.Family != other.Family {
		return fmt.Errorf("Type error, expecting %v, got %v", other, k)
	}
	if !other.Borrowed && k.Borrowed {
		return fmt.Errorf("Type error, expecting owned %v, but got %v", other, k)
	}
	return nil
}

func (k Kind) IsEquivalent(other Kind) error {
	if k.Family != other.Family || k.Borrowed != other.Borrowed {
		return fmt.Errorf("%v vs. %v", other, k)
	}
	return nil
}

type astHangTen struct {
	Imports   []*astImport   `EOL* @@*`
	Functions []*astFunction `     @@*`
}

type astImport struct {
	ModuleName string `"import" @Ident EOL+`
}

type astFunction struct {
	IsSynchronous bool      `@"sync"?`
	IsNative      bool      `@"native"?`
	Name          string    `'func' @Ident`
	Args          []*astArg `'(' @@* (',' @@*)* ')'`
	ReturnKind    []*Kind   `":" (@@ | "(" @@ ("," @@)* ")")`
	Block         *astBlock `@@? EOL+`
}

func (a *astFunction) ReturnValue(args []Kind) ([]*Kind, error) {
	if len(args) != len(a.Args) {
		return a.ReturnKind, fmt.Errorf("Type error: argument count mismatch, expecting %d, got %d", len(a.Args), len(args))
	}

	for i, arg := range a.Args {
		if err := args[i].CanConvertTo(*arg.Kind); err != nil {
			return a.ReturnKind, err
		}
	}

	return a.ReturnKind, nil
}

type astArg struct {
	Name string `@Ident`
	Kind *Kind  `':' @@`
}

type astBlock struct {
	Statements []*astStmt `'{' EOL* @@* '}'`
}

type astStmt struct {
	Let      *astLetStmt         `( @@`
	Return   *astReturnStmt      `| @@`
	Cond     *astConditionalStmt `| @@`
	Repeat   *astRepeatStmt      `| @@`
	BareExpr *astExpression      `| @@ ) EOL+`

	Pos    lexer.Position
	EndPos lexer.Position
}

type astLetStmt struct {
	MustExist bool           `("let" | @"set")`
	VarNames  []string       `@Ident ("," @Ident)*`
	Value     *astExpression `"=" @@`
}

type astReturnStmt struct {
	Value *astExpression `"return" @@`
}

type astRepeatStmt struct {
	Condition *astExpression `"while" @@`
	Block     *astBlock      `@@`
}

type astConditionalStmt struct {
	Cond      *astExpression `"if" @@`
	IfTrue    *astBlock      `@@`
	Otherwise *astBlock      `"else" @@`
}

type astMethodCall struct {
	Args []*astMethodArg `"(" @@ (',' @@)* ")"`
}

type astMethodArg struct {
	Borrow *string        `"&" @Ident`
	Expr   *astExpression `| @@`
}

type astExpression struct {
	Call  *astExpressionCall `@@`
	Terms []*astTerm         `@@*`
}

type astTerm struct {
	Operand *astExpressionCall `"+" @@*`
}

type astExpressionCall struct {
	Base  *astExpressionBase `@@`
	Calls []*astMethodCall   `@@*`
}

type astExpressionBase struct {
	Variable *string          `@Ident`
	String   *string          `| @String`
	Tuple    []*astExpression `| "(" @@ ("," @@)+ ")"`
	Integer  *int64           `| @Int`
}

type program struct {
	Functions          map[string]*astFunction
	GeneratedFunctions []*generator
}

var ufLexer = stateful.MustSimple([]stateful.Rule{
	{`Ident`, `[a-zA-Z][a-zA-Z_\d]*`, nil},
	{`String`, `"(?:\\.|[^"])*"`, nil},
	{`Int`, `\d+`, nil},
	{`EOL`, `[\r\n]`, nil},
	{"comment", `//[^\n]*`, nil},
	{"Punct", `[-[!@#$%^&*()+_={}\|:;"'<,>.?/]|]`, nil},
	{"whitespace", `[ \t]`, nil},
})

var parser = participle.MustBuild(
	&astHangTen{},
	participle.Lexer(ufLexer),
	participle.Unquote("String"))

func Parse(main string, sources map[string]string) (map[string]string, error) {
	program := &program{map[string]*astFunction{}, []*generator{}}

	queue := []string{main}
	nextQueue := []string{}

	for len(queue) > 0 {
		for _, mod := range queue {
			filename := mod + ".ht"
			input, ok := sources[filename]
			if !ok {
				return nil, fmt.Errorf("no such file: %s", filename)
			}

			t := &astHangTen{}
			if err := parser.ParseString(filename, input, t); err != nil {
				return nil, err
			}

			for _, imp := range t.Imports {
				nextQueue = append(nextQueue, imp.ModuleName)
			}

			for _, fun := range t.Functions {
				if _, ok := program.Functions[fun.Name]; ok {
					return nil, fmt.Errorf("function already exists: %s", fun.Name)
				}
				program.Functions[fun.Name] = fun
			}
		}
		queue, nextQueue = nextQueue, queue[:0]
	}

	if _, ok := program.Functions["main"]; !ok {
		return nil, fmt.Errorf("no main function defined in %s", main)
	}

	for _, fun := range program.Functions {
		if err := fun.Generate(program); err != nil {
			return nil, err
		}
	}

	outputFiles := map[string]string{}

	result := strings.Builder{}
	fmt.Fprintf(&result, "#include <stdbool.h>\n")
	fmt.Fprintf(&result, "#include \"../builtins.h\"\n")
	for _, defin := range program.GeneratedFunctions {
		defin.TypeDefinition(&result)
	}
	outputFiles[fmt.Sprintf("%s.h", main)] = result.String()

	result = strings.Builder{}
	fmt.Fprintf(&result, "#include \"%s.h\"\n", main)
	fmt.Fprintf(&result, "#include <stdlib.h>\n")
	fmt.Fprintf(&result, "#include <stdio.h>\n")
	fmt.Fprintf(&result, "#include <assert.h>\n")
	fmt.Fprintf(&result, "#include <string.h>\n")
	fmt.Fprintf(&result, "#include <stdint.h>\n")
	for _, defin := range program.GeneratedFunctions {
		defin.FormatInto(&result)
	}
	outputFiles[fmt.Sprintf("%s.c", main)] = result.String()
	return outputFiles, nil
}
