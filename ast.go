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
	"errors"
	"fmt"
)

type register int
type condition int
type childCall int

func (a *astConditionalStmt) Captures(out map[string]bool) {
	a.Cond.Captures(out)
	a.IfTrue.Captures(out)
	a.Otherwise.Captures(out)
}

func (a *astConditionalStmt) Generate(p *program, b *generator) error {
	cond, err := a.Cond.Generate(p, b)
	if err != nil {
		return err
	}
	if len(cond) != 1 {
		return errors.New("Got multiple values in condition")
	}
	condition := cond[0]

	typeAssertVarName := ""
	unionKind := (*Kind)(nil)
	unionRegister := register(0)

	if a.TypeAssertKind != nil {
		// Allow type assertions to narrow the type of a union
		if a.Cond.Comparison == nil && len(a.Cond.Sum.Terms) == 0 && len(a.Cond.Sum.Call.Calls) == 0 && a.Cond.Sum.Call.Base.Variable != nil {
			typeAssertVarName = *a.Cond.Sum.Call.Base.Variable
			unionRegister = b.Locals[typeAssertVarName]
			unionKind = b.Registers[unionRegister]
		}

		union := b.Registers[condition]
		if union.Family != FamilyUnion {
			return errors.New("Attempted to do a type switch on a non-union")
		}

		found := -1
		for i, option := range union.Args {
			if option.IsEquivalent(*a.TypeAssertKind) == nil {
				found = i
				break
			}
		}

		if found < 0 {
			return errors.New("Attempted to type switch on impossible type")
		}

		result := b.NewReg(&Kind{false, FamilyBoolean, nil}, true)
		b.Stmt(&genCheckUnionType{condition, found, result})

		condition = result

		// Delete the union type
		if typeAssertVarName != "" {
			b.Registers[unionRegister] = nil
			delete(b.Locals, typeAssertVarName)
		}
	}

	if err := b.Registers[condition].CanConvertTo(Kind{true, FamilyBoolean, nil}); err != nil {
		return err
	}

	parentCondition := b.CurrentCondition
	trueCondition := b.NewCondition()
	falseCondition := b.NewCondition()
	registers := make([]*Kind, len(b.Registers))
	copy(registers, b.Registers)

	b.Stmt(&genBranch{condition, trueCondition, falseCondition})

	localsAtStart := b.CopyOfLocals()
	localsBeforeTrue := b.CopyOfLocals()
	b.CurrentCondition = trueCondition

	if typeAssertVarName != "" {
		overwrittenReg := b.NewReg(a.TypeAssertKind, true)
		b.Stmt(&genExtractUnionValue{unionRegister, overwrittenReg})
		b.Locals[typeAssertVarName] = overwrittenReg
	}

	if err := a.IfTrue.Generate(p, b); err != nil {
		return err
	}

	localsAfterTrue := b.Locals
	b.Locals = localsBeforeTrue
	copy(b.Registers[:len(registers)], registers)
	localsBeforeFalse := b.CopyOfLocals()

	b.CurrentCondition = falseCondition

	// If the union is composed of exactly two values, use the remaining one.
	if typeAssertVarName != "" && len(unionKind.Args) == 2 {
		unionTypeArgs := unionKind.Args
		leftover := unionTypeArgs[0]
		if unionTypeArgs[0].IsEquivalent(*a.TypeAssertKind) == nil {
			leftover = unionTypeArgs[1]
		}

		overwrittenReg := b.NewReg(leftover, true)
		b.Stmt(&genExtractUnionValue{unionRegister, overwrittenReg})
		b.Locals[typeAssertVarName] = overwrittenReg
	}

	if err := a.Otherwise.Generate(p, b); err != nil {
		return err
	}

	b.CurrentCondition = parentCondition

	localsAfterFalse := b.Locals
	b.Locals = localsBeforeFalse

	for name := range b.Locals {
		regTrue, ok := localsAfterTrue[name]
		if !ok {
			delete(b.Locals, name)
			continue
		}

		regFalse, ok := localsAfterFalse[name]
		if !ok {
			delete(b.Locals, name)
			continue
		}

		if err := b.Registers[regTrue].IsEquivalent(*b.Registers[regFalse]); err != nil {
			return fmt.Errorf("%s has unequal types on both sides of if-statement: %w", err)
		}

		// If the variable is used on one side and not the other, make sure
		// that both sides have a unique variable name, so that any future
		// dependencies on this variable wait until the condition is resolved.
		if regTrue != regFalse {
			if regTrue == localsAtStart[name] {
				renamed := b.NewReg(b.Registers[regTrue], true)
				b.Conditions = append(b.Conditions, stmtWithCondition{trueCondition, &genRenameRegister{regTrue, renamed}})
				regTrue = renamed
			}

			if regFalse == localsAtStart[name] {
				renamed := b.NewReg(b.Registers[regFalse], true)
				b.Conditions = append(b.Conditions, stmtWithCondition{falseCondition, &genRenameRegister{regFalse, renamed}})
				regFalse = renamed
			}
		}

		b.JoinRegisters(regTrue, regFalse)
		b.Locals[name] = regTrue
	}

	return nil
}

func (a *astExpressionBase) Captures(out map[string]bool) {
	if a.Variable != nil {
		if *a.Variable != "true" && *a.Variable != "false" {
			out[*a.Variable] = true
		}
	} else if a.Tuple != nil {
		for _, ast := range a.Tuple {
			ast.Captures(out)
		}
	} else if a.IsArray {
		for _, ast := range a.Array {
			ast.Captures(out)
		}
	}
}

func (a *astExpressionBase) Generate(p *program, b *generator) ([]register, error) {
	if a.Variable != nil {
		if *a.Variable == "true" || *a.Variable == "false" {
			reg := b.NewReg(&Kind{false, FamilyBoolean, nil}, true)
			val := int64(0)
			if *a.Variable == "true" {
				val = 1
			}
			b.Stmt(&genIntegerLiteral{reg, val})
			return []register{reg}, nil
		}

		if v, ok := b.Locals[*a.Variable]; ok {
			return []register{v}, nil
		}
		return nil, fmt.Errorf("Unknown variable \"%s\"", *a.Variable)

	} else if a.String != nil {
		reg := b.NewReg(&Kind{true, FamilyString, nil}, true)
		b.Stmt(&genStringLiteral{reg, *a.String})
		return []register{reg}, nil

	} else if a.Integer != nil {
		reg := b.NewReg(&Kind{false, FamilyInteger, nil}, true)
		b.Stmt(&genIntegerLiteral{reg, *a.Integer})
		return []register{reg}, nil

	} else if a.Tuple != nil {
		result := []register{}
		for _, ast := range a.Tuple {
			regs, err := ast.Generate(p, b)
			if err != nil {
				return nil, err
			}
			if len(regs) != 1 {
				return nil, errors.New("Cannot use multi-variable value in tuple")
			}
			result = append(result, regs[0])
		}
		return result, nil

	} else if a.IsArray {
		result := []register{}
		kind := (*Kind)(nil)
		for _, ast := range a.Array {
			regs, err := ast.Generate(p, b)
			if err != nil {
				return nil, err
			}
			if len(regs) != 1 {
				return nil, errors.New("Cannot use multi-variable value in array")
			}
			mykind := b.Registers[regs[0]]
			if kind == nil {
				kind = mykind
			} else if err := kind.IsEquivalent(*mykind); err != nil {
				return nil, err
			}
			result = append(result, regs[0])
		}
		reg := b.NewReg(&Kind{false, FamilyArray, []*Kind{kind}}, true)
		b.Stmt(&genNewArray{reg, result})
		return []register{reg}, nil

	} else {
		return nil, fmt.Errorf("Unknown astExpressionBase %v", a)
	}
}

func (a *astMethodArg) Captures(out map[string]bool) {
	if a.Borrow != nil {
		out[*a.Borrow] = true
	} else {
		a.Expr.Captures(out)
	}
}

func (a *astMethodArg) Generate(p *program, b *generator) (reg register, borrow string, err error) {
	if a.Borrow != nil {
		var ok bool
		if reg, ok = b.Locals[*a.Borrow]; !ok {
			err = fmt.Errorf("Cannot borrow non-existing local variable %s", *a.Borrow)
			return
		}
		borrow = *a.Borrow
	} else {
		var regs []register
		if regs, err = a.Expr.Generate(p, b); err != nil {
			return
		}
		if len(regs) != 1 {
			err = fmt.Errorf("multi argument value passed as function arg")
			return
		}
		reg = regs[0]
	}
	return
}

func buildMethodCall(p *program, b *generator, calleeName string, args []*astMethodArg) ([]register, error) {
	callee, ok := p.Functions[calleeName]
	if !ok {
		return []register{}, fmt.Errorf("no function %s", calleeName)
	}

	kinds := []Kind{}
	registers := []register{}
	borrows := []string{}

	for i, arg := range args {
		reg, borrow, err := arg.Generate(p, b)
		if err != nil {
			return nil, err
		}

		registers = append(registers, reg)
		kinds = append(kinds, *b.Registers[reg])
		borrows = append(borrows, borrow)

		// Clear out all registers/local variables that were moved into this
		// function.
		if !callee.Args[i].Kind.Borrowed {
			for idx := range b.Registers {
				if r := register(idx); b.ResolveRegister(r) == reg {
					b.Registers[r] = nil
				}
			}

			for lcl, target := range b.Locals {
				if b.ResolveRegister(target) == reg {
					delete(b.Locals, lcl)
				}
			}
		}
	}

	resultKinds, err := callee.ReturnValue(kinds)
	if err != nil {
		return []register{}, err
	}

	for len(borrows) < len(resultKinds) {
		borrows = append(borrows, "")
	}

	results := []register{}
	for _, kind := range resultKinds {
		results = append(results, b.NewReg(kind, callee.IsSynchronous))
	}

	if callee.IsSynchronous {
		b.Stmt(&genCallSyncFunction{calleeName, registers, results})
	} else {
		b.Stmt(&genCallAsyncFunction{calleeName, registers, results, b.NewChildCall(calleeName)})
	}

	actualResults := []register{}
	for i, result := range results {
		if borrows[i] != "" {
			b.Locals[borrows[i]] = result
		} else {
			actualResults = append(actualResults, result)
		}
	}

	return actualResults, nil
}

func (a *astExpression) Captures(out map[string]bool) {
	a.Sum.Captures(out)
	if a.Comparison != nil {
		a.Comparison.Operand.Captures(out)
	}
}

func (a *astExpression) Generate(p *program, b *generator) ([]register, error) {
	if a.Comparison == nil {
		return a.Sum.Generate(p, b)
	}

	lhs, err := a.Sum.Generate(p, b)
	if err != nil {
		return nil, err
	}
	if len(lhs) != 1 {
		return nil, fmt.Errorf("expecting single valued lhs %v", lhs)
	}
	if err := b.Registers[lhs[0]].CanConvertTo(Kind{true, FamilyInteger, nil}); err != nil {
		return nil, err
	}

	rhs, err := a.Comparison.Operand.Generate(p, b)
	if err != nil {
		return nil, err
	}
	if len(rhs) != 1 {
		return nil, fmt.Errorf("expecting single valued rhs %v", rhs)
	}
	if err := b.Registers[rhs[0]].CanConvertTo(Kind{true, FamilyInteger, nil}); err != nil {
		return nil, err
	}

	result := b.NewReg(&Kind{false, FamilyBoolean, nil}, true)
	b.Stmt(&genIntegerComparison{Operation: a.Comparison.Cond, Left: lhs[0], Right: rhs[0], Result: result})
	return []register{result}, nil
}

func (a *astExpressionSum) Captures(out map[string]bool) {
	a.Call.Captures(out)
	for _, term := range a.Terms {
		term.Operand.Captures(out)
	}
}

func (a *astExpressionSum) Generate(p *program, b *generator) ([]register, error) {
	call := a.Call
	for _, term := range a.Terms {
		c := "concat"
		call = &astExpressionCall{
			Base: &astExpressionBase{Variable: &c},
			Calls: []*astMethodCall{{
				Args: []*astMethodArg{
					{Expr: &astExpression{Sum: &astExpressionSum{Call: call}}},
					{Expr: &astExpression{Sum: &astExpressionSum{Call: term.Operand}}},
				},
			}},
		}
	}
	return call.Generate(p, b)
}

func (a *astExpressionCall) Captures(out map[string]bool) {
	a.Base.Captures(out)
	for _, call := range a.Calls {
		for _, arg := range call.Args {
			arg.Captures(out)
		}
	}
}

func (a *astExpressionCall) Generate(p *program, b *generator) ([]register, error) {
	if len(a.Calls) == 0 {
		return a.Base.Generate(p, b)
	}

	if a.Base.Variable == nil || len(a.Calls) > 1 {
		return []register{}, fmt.Errorf("calls of non-immediate functions are unimplemented")
	}

	return buildMethodCall(p, b, *a.Base.Variable, a.Calls[0].Args)
}

func (a *astLetStmt) Captures(out map[string]bool) {
	a.Value.Captures(out)
}

func (a *astLetStmt) Generate(p *program, b *generator) error {
	for _, name := range a.VarNames {
		if _, ok := b.Locals[name]; ok != a.MustExist {
			if a.MustExist {
				return fmt.Errorf("Variable %s does not exist", name)
			} else {
				return fmt.Errorf("Variable %s already exists", name)
			}
		}
	}

	regs, err := a.Value.Generate(p, b)
	if err != nil {
		return err
	}

	if len(a.VarNames) == 1 {
		regs = []register{b.MaybeMakeTuple(regs)}
	}

	if len(regs) != len(a.VarNames) {
		return fmt.Errorf("Arity mismatch: %d versus %d", len(regs), len(a.VarNames))
	}

	for i, varName := range a.VarNames {
		b.Locals[varName] = regs[i]
	}
	return nil
}

func (a *astReturnStmt) Captures(out map[string]bool) {
	a.Value.Captures(out)
}

func (a *astReturnStmt) Generate(p *program, g *generator) error {
	regs, err := a.Value.Generate(p, g)
	if err != nil {
		return err
	}

	if len(g.ReturnKind) != len(regs) {
		return fmt.Errorf("arg count mismatch: %d vs. %d", len(g.ReturnKind), len(regs))
	}
	for i, reg := range regs {
		if err := g.Registers[reg].CanConvertTo(*g.ReturnKind[i]); err != nil {
			return err
		}
	}

	garbage, err := g.GarbageRegisters(regs)
	if err != nil {
		return err
	}

	g.Stmt(&genReturn{regs, garbage})
	return nil
}

func (a *astBlock) Captures(out map[string]bool) {
	for _, stmt := range a.Statements {
		stmt.Captures(out)
	}
}

func (a *astBlock) Generate(p *program, g *generator) error {
	for _, stmt := range a.Statements {
		if err := stmt.Generate(p, g); err != nil {
			return fmt.Errorf("%s: %w", stmt.Pos, err)
		}
	}
	return nil
}

func (a *astStmt) Captures(out map[string]bool) {
	if a.Let != nil {
		a.Let.Captures(out)
	} else if a.Return != nil {
		a.Return.Captures(out)
	} else if a.BareExpr != nil {
		a.BareExpr.Captures(out)
	} else if a.Cond != nil {
		a.Cond.Captures(out)
	} else if a.Repeat != nil {
		a.Repeat.Captures(out)
	} else {
		panic("unknown stmt type")
	}
}

func (a *astStmt) Generate(p *program, g *generator) error {
	if a.Let != nil {
		return a.Let.Generate(p, g)
	} else if a.Return != nil {
		return a.Return.Generate(p, g)
	} else if a.BareExpr != nil {
		regs, err := a.BareExpr.Generate(p, g)
		if err != nil {
			return err
		}
		if len(regs) != 0 {
			return fmt.Errorf("Expected void return type, got (unused) %v", regs)
		}
		return nil
	} else if a.Cond != nil {
		return a.Cond.Generate(p, g)
	} else if a.Repeat != nil {
		return a.Repeat.Generate(p, g)
	}
	return errors.New("Unknown astStmt type")
}

func (a *astRepeatStmt) Captures(out map[string]bool) {
	a.Block.Captures(out)
}

func (a *astRepeatStmt) Generate(p *program, g *generator) error {
	kinds := []*Kind{}
	names := []string{}
	registers := []register{}
	resultRegisters := []register{}

	captures := map[string]bool{}
	a.Block.Captures(captures)

	for name := range captures {
		reg, ok := g.Locals[name]
		if !ok {
			continue
		}
		names = append(names, name)
		registers = append(registers, reg)
		kind := g.Registers[reg]
		kinds = append(kinds, kind)
		resultRegisters = append(resultRegisters, g.NewReg(kind, false))
	}

	closureName := ""

	// Set up the closure to repeat after completion
	{
		closure := g.NewClosure(p, names, kinds, kinds)
		if err := a.Block.Generate(p, closure); err != nil {
			return err
		}
		childCall := closure.NewChildCall(closure.Name)

		cond, err := a.Condition.Generate(p, closure)
		if err != nil {
			return err
		}
		if len(cond) != 1 {
			return fmt.Errorf("got multiple values for while condition")
		}

		continueCondition := closure.NewCondition()
		exitCondition := closure.NewCondition()

		returnVariables := []register{}
		for i, lcl := range names {
			reg, ok := closure.Locals[lcl]
			if !ok {
				return fmt.Errorf("captured variable lost during loop: %s", lcl)
			}
			if err := closure.Registers[reg].IsEquivalent(*kinds[i]); err != nil {
				return fmt.Errorf("%s changed type during loop: %w", kinds[i], err)
			}
			returnVariables = append(returnVariables, reg)
		}

		garbage, err := closure.GarbageRegisters(returnVariables)
		if err != nil {
			return err
		}

		closure.StmtWithCond(0, &genBranch{cond[0], continueCondition, exitCondition})
		closure.StmtWithCond(continueCondition, &genRestartLoop{returnVariables, childCall, garbage})
		closure.StmtWithCond(exitCondition, &genReturn{returnVariables, garbage})

		closureName = closure.Name
	}

	startCondition := g.NewCondition()
	skipCondition := g.NewCondition()

	cond, err := a.Condition.Generate(p, g)
	if err != nil {
		return err
	}
	if len(cond) != 1 {
		return fmt.Errorf("got multiple values for while condition")
	}

	g.Stmt(&genBranch{cond[0], startCondition, skipCondition})
	g.StmtWithCond(startCondition, &genCallAsyncFunction{closureName, registers, resultRegisters, g.NewChildCall(closureName)})

	for i, name := range names {
		before := g.Locals[name]
		after := resultRegisters[i]

		if err := g.Registers[before].IsEquivalent(*g.Registers[after]); err != nil {
			return fmt.Errorf("%s changed type during loop: %w", err)
		}

		g.Registers[before] = nil
		g.StmtWithCond(skipCondition, &genRenameRegister{before, after})
		g.Locals[name] = after
	}

	return nil
}

func (a *astFunction) Generate(p *program) error {
	argNames := []string{}
	argKinds := []*Kind{}
	for _, arg := range a.Args {
		argNames = append(argNames, arg.Name)
		argKinds = append(argKinds, arg.Kind)
	}

	function := newGenerator(a.Name, p, argNames, argKinds, a.ReturnKind)
	function.IsNative = a.IsNative

	if a.Block != nil {
		if err := a.Block.Generate(p, function); err != nil {
			return err
		}
	}
	return nil
}
