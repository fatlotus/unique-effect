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
	"github.com/alecthomas/participle/v2/lexer"
	"io"
)

type stmtWithCondition struct {
	Cond      condition
	Statement generatedStatement
}

type generator struct {
	Name           string
	Conditions     []stmtWithCondition
	Locals         map[string]register
	ConsumedLocals map[string]*lexer.Position
	Results        int
	Registers      []*Kind
	IsNative       bool
	ArgKinds       []*Kind
	ReturnKind     []*Kind
	Substitutions  map[register]register
	ChildCalls     []string
	NextClosure    int

	CurrentCondition condition
	NextCondition    condition
}

func newGenerator(name string, program *program, argNames []string, argKinds []*Kind, results []*Kind) *generator {
	function := &generator{}
	function.Name = name
	function.Substitutions = map[register]register{}
	function.Locals = map[string]register{}
	function.ConsumedLocals = map[string]*lexer.Position{}
	function.ArgKinds = argKinds
	function.ReturnKind = results
	function.Results = len(results)

	function.Conditions = []stmtWithCondition{}
	function.CurrentCondition = 0

	for i, arg := range argNames {
		function.Registers = append(function.Registers, argKinds[i])
		function.Locals[arg] = register(i)
	}

	program.GeneratedFunctions = append(program.GeneratedFunctions, function)
	return function
}

func (g generator) ResolveRegister(r register) register {
	for {
		r2, ok := g.Substitutions[r]
		if !ok {
			return r
		}
		if r2 >= r {
			panic("Substitutions should always decrease")
		}
		r = r2
	}
}

func (g generator) Reg(r register) string {
	return fmt.Sprintf("sp->r[%d]", g.ResolveRegister(r))
}

func (g *generator) JoinRegisters(a, b register) {
	a = g.ResolveRegister(a)
	b = g.ResolveRegister(b)
	if a == b {
		return
	}
	if a < b {
		g.Substitutions[b] = a
	} else {
		g.Substitutions[a] = b
	}
}

func (g *generator) Stmt(s generatedStatement) {
	g.StmtWithCond(g.CurrentCondition, s)
}

func (g *generator) StmtWithCond(c condition, s generatedStatement) {
	g.Conditions = append(g.Conditions, stmtWithCondition{c, s})
}

func (g *generator) NewReg(k *Kind, immediate bool) register {
	reg := register(len(g.Registers))
	g.Registers = append(g.Registers, k)
	return reg
}

func (g *generator) CopyOfLocals() map[string]register {
	result := map[string]register{}
	for name, reg := range g.Locals {
		result[name] = reg
	}
	return result
}

func (g *generator) GarbageRegisters(keep []register) (map[register]*Kind, error) {
	keepMap := map[register]bool{}
	for _, reg := range keep {
		keepMap[g.ResolveRegister(reg)] = true
	}

	garbage := map[register]*Kind{}
	for index, kind := range g.Registers {
		reg := g.ResolveRegister(register(index))
		if kind != nil && !keepMap[reg] && kind.NeedsToBeDeleted() {
			if kind.CanBeImplicitlyDeleted() {
				garbage[reg] = kind
			} else {
				return nil, fmt.Errorf("unused value of type %s (r%d)", kind, reg)
			}
		}
	}
	return garbage, nil
}

func (g generator) TypeDefinition(w io.Writer) {
	if g.IsNative {
		fmt.Fprintf(w, "void unique_effect_%s();\n", g.Name)
		return
	}
	fmt.Fprintf(w, "struct unique_effect_%s_state {\n", g.Name)
	fmt.Fprintf(w, "  future_t r[%d];\n", len(g.Registers))
	fmt.Fprintf(w, "  future_t *result[%d];\n", g.Results)
	fmt.Fprintf(w, "  closure_t caller;\n")
	fmt.Fprintf(w, "  bool conditions[%d];\n", len(g.Conditions))
	for index, kind := range g.ChildCalls {
		fmt.Fprintf(w, "  struct unique_effect_%s_state *call_%d;\n", kind, index)
		fmt.Fprintf(w, "  bool call_%d_done;\n", index)
	}
	fmt.Fprintf(w, "};\n")
	fmt.Fprintf(w, "%s;\n", g.Header())
}

func (g *generator) DumpRegisters(w io.Writer) {
	fmt.Fprintf(w, "  fprintf(stderr, \"%15s %%p ready=", g.Name)
	for range g.Conditions {
		fmt.Fprintf(w, "%%s")
	}
	for range g.Registers {
		fmt.Fprintf(w, "%%s")
	}
	fmt.Fprintf(w, "\\n\", sp")

	for i := range g.Conditions {
		fmt.Fprintf(w, ", (sp->conditions[%[1]d] ? \"cond%[1]d \" : \"\")", i)
	}

	for i := range g.Registers {
		localName := ""
		for lcl, reg := range g.Locals {
			if reg == register(i) {
				localName = "(" + lcl + ")"
			}
		}
		fmt.Fprintf(w, ", (%s.ready ? \"r%d%s \" : \"\")", g.Reg(register(i)), i, localName)
	}
	fmt.Fprintf(w, ");\n")
}

func (g *generator) FormatInto(w io.Writer) {
	if g.IsNative {
		return
	}

	fmt.Fprintf(w, "%s {\n", g.Header())

	fmt.Fprintf(w, "  if (!sp->conditions[0]) {\n")
	fmt.Fprintf(w, "    memset(&sp->conditions, '\\0', sizeof(sp->conditions));\n")
	fmt.Fprintf(w, "    sp->conditions[0] = true;\n")
	for i := range g.ChildCalls {
		fmt.Fprintf(w, "    sp->call_%d = NULL;\n", i)
		fmt.Fprintf(w, "    sp->call_%d_done = false;\n", i)
	}
	fmt.Fprintf(w, "  }\n")

	// g.DumpRegisters(w)

	for _, stmtWithCondition := range g.Conditions {
		condition := stmtWithCondition.Cond
		stmt := stmtWithCondition.Statement

		fmt.Fprintf(w, "  // %#v\n", stmt)

		if condition > 0 {
			fmt.Fprintf(w, "  if (sp->conditions[%d]", condition)
		} else {
			fmt.Fprintf(w, "  if (true")
		}

		needs, provides := stmt.Deps()
		for _, need := range needs {
			fmt.Fprintf(w, " && %s.ready", g.Reg(need))
		}
		for _, provide := range provides {
			fmt.Fprintf(w, " && !%s.ready", g.Reg(provide))
		}
		fmt.Fprintf(w, ") {\n")

		fmt.Fprintf(w, "%s", stmt.Generate(g))
		fmt.Fprintf(w, "  }\n")
	}

	// g.DumpRegisters(w)

	fmt.Fprintf(w, "}\n")
}

func (g *generator) FormatMainInto(w io.Writer) error {
	fmt.Fprintf(w, "int main(int argc, const char* argv[]) {\n")
	fmt.Fprintf(w, "  struct unique_effect_runtime rt;\n")
	fmt.Fprintf(w, "  unique_effect_runtime_init(&rt);\n")
	fmt.Fprintf(w, "  struct unique_effect_%[1]s_state *st = calloc(1, sizeof(struct unique_effect_%[1]s_state));\n", g.Name)

	for i, kind := range g.ArgKinds {
		if kind.CanBeArgumentToMain() {
			fmt.Fprintf(w, "  st->r[%d].value = kSingleton%s;\n", i, kind.Family.String())
			fmt.Fprintf(w, "  st->r[%d].ready = true;\n", i)
		} else {
			return fmt.Errorf("not sure how to synthesize a %s", *kind)
		}
	}

	for i, kind := range g.ReturnKind {
		if kind.CanBeReturnedFromMain() {
			fmt.Fprintf(w, "  future_t dropped_result_%d;\n", i)
			fmt.Fprintf(w, "  st->result[%[1]d] = &dropped_result_%[1]d;\n", i)
		} else {
			return fmt.Errorf("not sure how to consume a %s", *kind)
		}
	}

	fmt.Fprintf(w, "  st->caller = (closure_t){.state = NULL, .func = &unique_effect_exit};\n")
	fmt.Fprintf(w, "  unique_effect_runtime_schedule(&rt, (closure_t){.state = st, .func = &unique_effect_%s});\n", g.Name)
	fmt.Fprintf(w, "  unique_effect_runtime_loop(&rt);\n")
	fmt.Fprintf(w, "}\n")
	return nil
}

func (g generator) Header() string {
	return fmt.Sprintf("void unique_effect_%s(struct unique_effect_runtime *rt, struct unique_effect_%s_state *sp)", g.Name, g.Name)
}

func (g *generator) NewClosure(p *program, argNames []string, argKinds []*Kind, results []*Kind) *generator {
	g.NextClosure += 1
	return newGenerator(fmt.Sprintf("%s_%d", g.Name, g.NextClosure), p, argNames, argKinds, results)
}

func (g *generator) NewCondition() condition {
	g.NextCondition += 1
	return g.NextCondition
}

func (g *generator) NewChildCall(name string) childCall {
	g.ChildCalls = append(g.ChildCalls, name)
	return childCall(len(g.ChildCalls) - 1)
}

func (g *generator) MaybeMakeTuple(registers []register) register {
	if len(registers) == 1 {
		return registers[0]
	} else {
		types := []*Kind{}
		for _, reg := range registers {
			types = append(types, g.Registers[reg])
		}
		result := g.NewReg(&Kind{false, FamilyTuple, types, "Tuple"}, true)
		g.Stmt(&genMakeTuple{Inputs: registers, Result: result})
		return result
	}
}

func (g *generator) Consume(reg register, position *lexer.Position) {
	for idx := range g.Registers {
		if r := register(idx); g.ResolveRegister(r) == reg {
			g.Registers[r] = nil
		}
	}

	for lcl, target := range g.Locals {
		if g.ResolveRegister(target) == reg {
			g.ConsumedLocals[lcl] = position
			delete(g.Locals, lcl)
		}
	}
}
