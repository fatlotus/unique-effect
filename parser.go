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

type TypeRep struct {
	Borrowed bool       `@"&"?`
	Name     string     `@Ident`
	Args     []*TypeRep `("[" @@ ("," @@)* "]")?`
}

type Family int

type Kind struct {
	Borrowed         bool
	Family           Family
	TupleOrUnionArgs []*Kind
	Label            string
}

const (
	FamilyString Family = iota
	FamilyStream
	FamilyClock
	FamilyTuple
	FamilyInteger
	FamilyBoolean
	FamilyArray
	FamilyFileSystem
	FamilyUnion
	FamilyCustom
)

func (f Family) String() string {
	switch f {
	case FamilyStream:
		return "Stream"
	case FamilyString:
		return "String"
	case FamilyClock:
		return "Clock"
	case FamilyTuple:
		return "Tuple"
	case FamilyInteger:
		return "Integer"
	case FamilyBoolean:
		return "Boolean"
	case FamilyArray:
		return "Array"
	case FamilyFileSystem:
		return "FileSystem"
	case FamilyUnion:
		return "Union"
	case FamilyCustom:
		return "Custom"
	default:
		return "?? Unknown"
	}
}

func CaptureFamily(name string) (Family, error) {
	switch name {
	case "String":
		return FamilyString, nil
	case "Stream":
		return FamilyStream, nil
	case "Clock":
		return FamilyClock, nil
	case "Integer":
		return FamilyInteger, nil
	case "Boolean":
		return FamilyBoolean, nil
	case "Array":
		return FamilyArray, nil
	case "FileSystem":
		return FamilyFileSystem, nil
	case "Union":
		return FamilyUnion, nil
	default:
		return FamilyCustom, nil
	}
}

func (k Kind) String() string {
	result := ""
	if k.Borrowed {
		result += "&"
	}
	result += k.Label
	if len(k.TupleOrUnionArgs) > 0 {
		result += "["
		for i, arg := range k.TupleOrUnionArgs {
			if i > 0 {
				result += ", "
			}
			result += arg.String()
		}
		result += "]"
	}
	return result
}

func (k Kind) CanConvertTo(other Kind) error {
	if k.Family != other.Family || k.Label != other.Label {
		return fmt.Errorf("Type error, expecting %v, got %s", other, k.String())
	}
	if !other.Borrowed && k.Borrowed {
		return fmt.Errorf("Type error, expecting owned %v, but got %v", other, k)
	}
	return nil
}

func (k Kind) IsEquivalent(other Kind) error {
	if k.Family != other.Family || k.Label != other.Label || k.Borrowed != other.Borrowed {
		return fmt.Errorf("%v vs. %v", other, k)
	}
	return nil
}

func (k Kind) NeedsToBeDeleted() bool {
	return !k.IsPrimitive() && !k.Borrowed
}

func (k Kind) CanBeImplicitlyDeleted() bool {
	return k.Family == FamilyString || k.Family == FamilyArray
}

func (k Kind) IsPrimitive() bool {
	return k.Family == FamilyInteger || k.Family == FamilyBoolean
}

func (k Kind) IsNumeric() bool {
	return k.Family == FamilyInteger
}

func (k Kind) IsBooleanLike() bool {
	return k.Family == FamilyBoolean
}

func (k Kind) CanBeArgumentToMain() bool {
	return k.Family == FamilyClock || k.Family == FamilyStream || k.Family == FamilyFileSystem
}

func (k Kind) CanBeReturnedFromMain() bool {
	return k.Family == FamilyClock || k.Family == FamilyStream || k.Family == FamilyFileSystem
}

func (k Kind) UnpackAsTuple() []*Kind {
	return append([]*Kind{}, k.TupleOrUnionArgs...)
}

func (k Kind) UnpackAsUnion() []*Kind {
	return append([]*Kind{}, k.TupleOrUnionArgs...)
}

type astHangTen struct {
	Imports     []*astImport           `EOL* @@*`
	Definitions []*astFunctionOrStruct `     @@*`
}

type astImport struct {
	ModuleName string `"import" @Ident EOL+`
}

type astFunctionOrStruct struct {
	Function *astFunction `  @@`
	Struct   *astStruct   `| @@`
}

type astStruct struct {
	Name   string     `"struct" @Ident`
	Fields []*TypeRep `"{" (EOL+ (@@ EOL+)+)? "}" EOL+`
}

type astFunction struct {
	IsSynchronous bool       `@"sync"?`
	IsNative      bool       `@"native"?`
	Name          string     `'func' @Ident`
	Args          []*astArg  `'(' @@* (',' @@*)* ')'`
	ReturnKind    []*TypeRep `":" (@@ | "(" @@ ("," @@)* ")")`
	Block         *astBlock  `@@? EOL+`
}

func (a *astFunction) ReturnValue(p *program, args []*Kind) ([]*Kind, error) {
	if len(args) != len(a.Args) {
		return nil, fmt.Errorf("Type error: argument count mismatch, expecting %d, got %d", len(a.Args), len(args))
	}

	for i, arg := range a.Args {
		resolved, err := p.ResolveType(arg.Kind)
		if err != nil {
			return nil, err
		}
		if err := args[i].CanConvertTo(*resolved); err != nil {
			return nil, err
		}
	}

	result := []*Kind{}
	for _, rep := range a.ReturnKind {
		resolved, err := p.ResolveType(rep)
		if err != nil {
			return nil, err
		}
		result = append(result, resolved)
	}

	return result, nil
}

type astArg struct {
	Name string   `@Ident`
	Kind *TypeRep `':' @@`
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
	Cond           *astExpression `"if" @@`
	TypeAssertKind *TypeRep       `("is" @@)?`
	IfTrue         *astBlock      `@@`
	Otherwise      *astBlock      `"else" @@`
}

type astMethodCall struct {
	Args []*astMethodArg `"(" @@ (',' @@)* ")"`
}

type astMethodArg struct {
	Borrow *string        `  "&" @Ident`
	Expr   *astExpression `| @@`

	Pos lexer.Position
}

type astExpression struct {
	Sum        *astExpressionSum `@@`
	Comparison *astComparison    `@@?`

	Pos lexer.Position
}

type astComparison struct {
	Cond    string            `@(">=" | "<=" | "<" | ">")`
	Operand *astExpressionSum `@@`
}

type astExpressionSum struct {
	Call  *astExpressionCall `@@`
	Terms []*astTerm         `@@*`
}

type astTerm struct {
	Op      string             `@"+"`
	Operand *astExpressionCall `@@`
}

type astExpressionCall struct {
	Base  *astExpressionBase `@@`
	Calls []*astMethodCall   `@@*`
}

type astExpressionBase struct {
	Variable        *string          `  @Ident`
	StructArguments []*astExpression `  ("{" @@ ("," @@)+ "}")?`
	String          *string          `| @String`
	Tuple           []*astExpression `| "(" @@ ("," @@)+ ")"`
	Integer         *int64           `| @Int`
	IsArray         bool             `| @("["`
	Array           []*astExpression `  (@@ ("," @@)*)? "]")`

	Pos lexer.Position
}

type program struct {
	Functions          map[string]*astFunction
	GeneratedFunctions []*generator
	Types              map[string][]*TypeRep
}

func (p *program) MustResolveBuiltinType(label string) *Kind {
	kind, err := p.ResolveType(&TypeRep{label == "String", label, nil})
	if err != nil {
		panic(err)
	}
	return kind
}

func (p *program) ResolveType(t *TypeRep) (*Kind, error) {
	var (
		family Family
		args   []*Kind
	)

	if t.Name == "Union" || t.Name == "Tuple" || t.Name == "Array" {
		// Generic type (has type arguments)
		if t.Name == "Union" {
			family = FamilyUnion
		} else if t.Name == "Tuple" {
			family = FamilyTuple
		} else if t.Name == "Array" {
			family = FamilyArray
		}

		for _, arg := range t.Args {
			resolved, err := p.ResolveType(arg)
			if err != nil {
				return nil, err
			}
			args = append(args, resolved)
		}

	} else {
		// Regular type (with no args)
		if len(t.Args) > 0 {
			return nil, fmt.Errorf("type %s doesn't take arguments", t.Name)
		}

		fields, ok := p.Types[t.Name]
		if !ok {
			return nil, fmt.Errorf("unknown type %s", t.Name)
		}

		// If the type has fields, fill them in as though they were type arguments
		for _, field := range fields {
			resolved, err := p.ResolveType(field)
			if err != nil {
				return nil, err
			}
			args = append(args, resolved)
		}

		if len(fields) > 0 {
			family = FamilyTuple
		} else {
			fam, err := CaptureFamily(t.Name)
			if err != nil {
				return nil, err
			}
			family = fam
		}
	}

	return &Kind{
		Borrowed:         t.Borrowed,
		Family:           family,
		TupleOrUnionArgs: args,
		Label:            t.Name,
	}, nil
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
	program := &program{map[string]*astFunction{}, []*generator{}, map[string][]*TypeRep{}}

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

			for _, defn := range t.Definitions {
				if fun := defn.Function; fun != nil {
					if _, ok := program.Functions[fun.Name]; ok {
						return nil, fmt.Errorf("function already exists: %s", fun.Name)
					}
					program.Functions[fun.Name] = fun
				} else {
					strct := defn.Struct
					if _, ok := program.Types[strct.Name]; ok {
						return nil, fmt.Errorf("type already exists: %s", strct.Name)
					}
					program.Types[strct.Name] = strct.Fields
				}
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
		if defin.Name == "main" {
			if err := defin.FormatMainInto(&result); err != nil {
				return nil, err
			}
		}
	}
	outputFiles[fmt.Sprintf("%s.c", main)] = result.String()
	return outputFiles, nil
}
