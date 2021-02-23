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
	"io"
	"strings"
)

type generatedStatement interface {
	Generate(block *generator) string
	Deps() ([]register, []register)
}

func freeGarbage(gen *generator, garbage map[register]*Kind, w io.Writer) {
	for reg, kind := range garbage {
		fmt.Fprintf(w, "        if (%[1]s.ready) free(%[1]s.value); // %[2]s\n", gen.Reg(reg), kind)
	}
}

type genRenameRegister struct {
	Source, Destination register
}

func (g *genRenameRegister) Generate(gen *generator) string {
	return fmt.Sprintf("    %s = %s;\n", gen.Reg(g.Destination), gen.Reg(g.Source))
}

func (g *genRenameRegister) Deps() ([]register, []register) {
	return []register{g.Source}, []register{g.Destination}
}

type genStringLiteral struct {
	Target register
	Value  string
}

func (g *genStringLiteral) Generate(gen *generator) string {
	return fmt.Sprintf("    %s = (future_t){.value = %#v, .ready = true};\n", gen.Reg(g.Target), g.Value)
}

func (g *genStringLiteral) Deps() ([]register, []register) {
	return nil, []register{g.Target}
}

type genIntegerLiteral struct {
	Target register
	Value  int64
}

func (g *genIntegerLiteral) Generate(gen *generator) string {
	return fmt.Sprintf("    %s = (future_t){.value = (void*)(intptr_t)%d, .ready = true};\n", gen.Reg(g.Target), g.Value)
}

func (g *genIntegerLiteral) Deps() ([]register, []register) {
	return nil, []register{g.Target}
}

type genCallSyncFunction struct {
	Name   string
	Args   []register
	Result []register
}

func (g *genCallSyncFunction) Generate(gen *generator) string {
	cArgs := []string{"rt"}
	for _, arg := range g.Args {
		cArgs = append(cArgs, fmt.Sprintf("%s.value", gen.Reg(arg)))
	}
	for _, ret := range g.Result {
		cArgs = append(cArgs, fmt.Sprintf("&%s.value", gen.Reg(ret)))
	}
	result := fmt.Sprintf("    unique_effect_%s(%s);\n", g.Name, strings.Join(cArgs, ", "))
	for _, ret := range g.Result {
		result += fmt.Sprintf("    %s.ready = true;\n", gen.Reg(ret))
	}
	return result
}

func (g *genCallSyncFunction) Deps() ([]register, []register) {
	return g.Args, g.Result
}

type genCallAsyncFunction struct {
	Name      string
	Args      []register
	Result    []register
	ChildCall childCall
}

func (g *genCallAsyncFunction) Generate(gen *generator) string {
	var result strings.Builder
	fmt.Fprintf(&result, "    if (sp->call_%d == NULL) {\n", g.ChildCall)
	fmt.Fprintf(&result, "      sp->call_%d = malloc(sizeof(struct unique_effect_%s_state));\n",
		g.ChildCall, g.Name)

	for i, arg := range g.Args {
		fmt.Fprintf(&result, "      sp->call_%d->r[%d] = %s;\n", g.ChildCall, i, gen.Reg(arg))
	}
	for i, ret := range g.Result {
		fmt.Fprintf(&result, "      sp->call_%d->result[%d] = &%s;\n", g.ChildCall, i, gen.Reg(ret))
	}

	fmt.Fprintf(&result, "      sp->call_%d->caller.func = &unique_effect_%s;\n", g.ChildCall, gen.Name)
	fmt.Fprintf(&result, "      sp->call_%d->caller.state = sp;\n", g.ChildCall)
	fmt.Fprintf(&result, "      sp->call_%d->conditions[0] = false;\n", g.ChildCall)
	fmt.Fprintf(&result, "      unique_effect_runtime_schedule(rt, (closure_t){.state = sp->call_%d, .func = &unique_effect_%s});\n", g.ChildCall, g.Name)
	fmt.Fprintf(&result, "    }\n")
	return result.String()
}

func (g *genCallAsyncFunction) Deps() ([]register, []register) {
	return g.Args, g.Result
}

type genRestartLoop struct {
	Args      []register
	ChildCall childCall
	Garbage   map[register]*Kind
}

func (g *genRestartLoop) Generate(gen *generator) string {
	var result strings.Builder
	fmt.Fprintf(&result, "    if (!sp->call_%d_done) {\n", g.ChildCall)
	fmt.Fprintf(&result, "      if (sp->call_%d == NULL) {\n", g.ChildCall)
	fmt.Fprintf(&result, "        sp->call_%d = malloc(sizeof(struct unique_effect_%s_state));\n",
		g.ChildCall, gen.Name)
	for i := range gen.ReturnKind {
		fmt.Fprintf(&result, "        sp->call_%d->result[%d] = sp->result[%d];\n", g.ChildCall, i, i)
	}

	fmt.Fprintf(&result, "        sp->call_%d->caller = sp->caller;\n", g.ChildCall)
	fmt.Fprintf(&result, "        sp->call_%d->conditions[0] = false;\n", g.ChildCall)
	fmt.Fprintf(&result, "      }\n")

	for i, arg := range g.Args {
		fmt.Fprintf(&result, "      sp->call_%d->r[%d] = %s;\n", g.ChildCall, i, gen.Reg(arg))
	}

	fmt.Fprintf(&result, "      unique_effect_runtime_schedule(rt, (closure_t){.state = sp->call_%d, .func = &unique_effect_%s});\n", g.ChildCall, gen.Name)

	cArgs := []string{}
	for _, arg := range g.Args {
		cArgs = append(cArgs, fmt.Sprintf("%s.ready", gen.Reg(arg)))
	}
	fmt.Fprintf(&result, "      if (%s) {\n", strings.Join(cArgs, " && "))
	fmt.Fprintf(&result, "        sp->call_%d_done = true;\n", g.ChildCall)

	freeGarbage(gen, g.Garbage, &result)
	fmt.Fprintf(&result, "        free(sp);\n")
	fmt.Fprintf(&result, "        return;\n")
	fmt.Fprintf(&result, "      }\n")
	fmt.Fprintf(&result, "    };\n")
	return result.String()
}

func (g *genRestartLoop) Deps() ([]register, []register) {
	return nil, nil // g.Args, nil
}

type genComment struct {
	Message string
}

func (g *genComment) Generate(gen *generator) string {
	return fmt.Sprintf("    // %s\n", g.Message)
}

func (g *genComment) Deps() ([]register, []register) {
	return nil, nil
}

type genReturn struct {
	ReturnValue []register
	Garbage     map[register]*Kind
}

func (g *genReturn) Generate(gen *generator) string {
	b := strings.Builder{}
	for i, reg := range g.ReturnValue {
		fmt.Fprintf(&b, "    *sp->result[%d] = %s;\n", i, gen.Reg(reg))
	}
	fmt.Fprintf(&b, "    unique_effect_runtime_schedule(rt, sp->caller);\n")

	freeGarbage(gen, g.Garbage, &b)

	// gen.DumpRegisters(&b)
	fmt.Fprintf(&b, "    free(sp);\n")
	fmt.Fprintf(&b, "    return;\n")
	return b.String()
}

func (g *genReturn) Deps() ([]register, []register) {
	return g.ReturnValue, nil
}

type genBranch struct {
	Condition register
	IfTrue    condition
	IfFalse   condition
}

func (g *genBranch) Generate(gen *generator) string {
	b := strings.Builder{}
	fmt.Fprintf(&b, "    if (%s.value != 0) {\n", gen.Reg(g.Condition))
	fmt.Fprintf(&b, "      sp->conditions[%d] = true;\n", g.IfTrue)
	fmt.Fprintf(&b, "    } else {\n")
	fmt.Fprintf(&b, "      sp->conditions[%d] = true;\n", g.IfFalse)
	fmt.Fprintf(&b, "    }\n")
	return b.String()
}

func (g *genBranch) Deps() ([]register, []register) {
	return []register{g.Condition}, nil
}

type genIntegerComparison struct {
	Operation string
	Left      register
	Right     register
	Result    register
}

func (g *genIntegerComparison) Generate(gen *generator) string {
	b := strings.Builder{}
	fmt.Fprintf(&b, "    %s.value = %s.value %s %s.value ? (void *)1 : (void *)0;\n", gen.Reg(g.Result), gen.Reg(g.Left), g.Operation, gen.Reg(g.Right))
	fmt.Fprintf(&b, "    %s.ready = true;\n", gen.Reg(g.Result))
	return b.String()
}

func (g *genIntegerComparison) Deps() ([]register, []register) {
	return []register{g.Left, g.Right}, []register{g.Result}
}
