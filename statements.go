package hang10

import (
	"fmt"
	"strings"
)

type generatedStatement interface {
	Generate(block *generator) string
	Deps() ([]register, []register)
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
	return fmt.Sprintf("    %s = (future_t){.value = %s, .ready = true};\n", gen.Reg(g.Target), g.Value)
}

func (g *genStringLiteral) Deps() ([]register, []register) {
	return nil, []register{g.Target}
}

type genIntegerLiteral struct {
	Target register
	Value  int64
}

func (g *genIntegerLiteral) Generate(gen *generator) string {
	return fmt.Sprintf("    %s = (future_t){.value = (void*)(uintptr_t)%d, .ready = true};\n", gen.Reg(g.Target), g.Value)
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
	cArgs := []string{}
	for _, arg := range g.Args {
		cArgs = append(cArgs, fmt.Sprintf("%s.value", gen.Reg(arg)))
	}
	for _, ret := range g.Result {
		cArgs = append(cArgs, fmt.Sprintf("&%s.value", gen.Reg(ret)))
	}
	result := fmt.Sprintf("    hang10_%s(%s);\n", g.Name, strings.Join(cArgs, ", "))
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
	fmt.Fprintf(&result, "      sp->call_%d = malloc(sizeof(struct hang10_%s_state));\n",
		g.ChildCall, g.Name)

	for i, arg := range g.Args {
		fmt.Fprintf(&result, "      sp->call_%d->r[%d] = %s;\n", g.ChildCall, i, gen.Reg(arg))
	}
	for i, ret := range g.Result {
		fmt.Fprintf(&result, "      sp->call_%d->result[%d] = &%s;\n", g.ChildCall, i, gen.Reg(ret))
	}

	fmt.Fprintf(&result, "      sp->call_%d->caller.func = &hang10_%s;\n", g.ChildCall, gen.Name)
	fmt.Fprintf(&result, "      sp->call_%d->caller.state = sp;\n", g.ChildCall)
	fmt.Fprintf(&result, "      sp->call_%d->conditions[0] = false;\n", g.ChildCall)
	fmt.Fprintf(&result, "      hang10_runtime_schedule(rt, (closure_t){.state = sp->call_%d, .func = &hang10_%s});\n", g.ChildCall, g.Name)
	fmt.Fprintf(&result, "    }\n")
	return result.String()
}

func (g *genCallAsyncFunction) Deps() ([]register, []register) {
	return g.Args, g.Result
}

type genRestartLoop struct {
	Args      []register
	ChildCall childCall
}

func (g *genRestartLoop) Generate(gen *generator) string {
	var result strings.Builder
	fmt.Fprintf(&result, "    if (!sp->call_%d_done) {\n", g.ChildCall)
	fmt.Fprintf(&result, "      if (sp->call_%d == NULL) {\n", g.ChildCall)
	fmt.Fprintf(&result, "        sp->call_%d = malloc(sizeof(struct hang10_%s_state));\n",
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
	cArgs := []string{}
	for _, arg := range g.Args {
		cArgs = append(cArgs, fmt.Sprintf("%s.ready", gen.Reg(arg)))
	}
	fmt.Fprintf(&result, "      if (%s) { sp->call_%d_done = true; }\n", strings.Join(cArgs, " && "), g.ChildCall)

	fmt.Fprintf(&result, "      hang10_runtime_schedule(rt, (closure_t){.state = sp->call_%d, .func = &hang10_%s});\n", g.ChildCall, gen.Name)
	fmt.Fprintf(&result, "    };\n")
	// for _, arg := range g.Args {
	// 	fmt.Fprintf(&result, "    if (!%s.ready) return;\n", gen.Reg(arg))
	// }
	// fmt.Fprintf(&result, "    free(sp);\n")
	// fmt.Fprintf(&result, "    return;\n")
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
}

func (g *genReturn) Generate(gen *generator) string {
	b := strings.Builder{}
	for i, reg := range g.ReturnValue {
		fmt.Fprintf(&b, "    *sp->result[%d] = %s;\n", i, gen.Reg(reg))
	}
	fmt.Fprintf(&b, "    hang10_runtime_schedule(rt, sp->caller);\n")
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

type genFree struct {
	Register register
}

func (g *genFree) Generate(gen *generator) string {
	return fmt.Sprintf("    fprintf(stderr, \"free(%s)\\n\");\n", gen.Reg(g.Register))
	// return "" // fmt.Sprintf("    free(%s.value); %s.ready = false;\n", gen.Reg(g.Register), gen.Reg(g.Register))
}

func (g *genFree) Deps() ([]register, []register) {
	return []register{g.Register}, nil
}
