/*
Copyright 2019 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package formatter

import (
	"github.com/google/go-jsonnet/ast"
	iast "github.com/google/go-jsonnet/internal/ast"
	"github.com/google/go-jsonnet/internal/pass"
)

// AddPlusObject is a formatter pass that replaces e {} with e + {}.
type AddPlusObject struct {
	pass.Base
}

// Context for AddPlusObject is the parent node. This is used to determine the
// operator precedence of the context in which a ApplyBrace is replaced with Binary{+},
// to decide whether it's necessary to introduce parentheses.
//
// Consider various cases:
//
//     {a:1} {b:2}  =>
//         No parens needed (context is top-level)
//     {a:1} {b:2}(42)  =>
//         Need parens. Context is an Apply, which binds tighter than operator +,
//         so {a:1}+{b:2}(42) without parens would have the wrong parse tree.
//     {a:1} {b:2}.a  =>
//         Need parens. Context is an Index operation, same as Apply.
//     {a:1} + {b:2} {c:3}  =>
//         Need parens. Context is Binary{+}, and we're on the right. If this was
//         formatted as {a:1} + {b:2} + {c:3} then it would parse differently
//         (because + is left-associative).
//     {a:1} {b:2} + {c:3}  =>
//         No parens! Context is Binary{+}, but we're on the left. Since plus is
//         left-associative anyway, {a:1} + {b:2} + {c:3} produces the same parse
//         tree without adding parens.
//     + 42 {b:2}  =>
//         Needs parens! The actual parse tree is (+ (implicit-plus 42 {b:2})),
//         so the ApplyBrace is in the context of a Unary{+}. Explicit plus
//         binds looser than unary plus so parens are needed to keep the same
//         parse tree.
//         The expression won't evaluate correctly (you can't add an object to
//         a number), but it still _parses_ so we need to format it correctly.
//
// Things to note here:
// - Since this is formatting not evaluation, we need to cope with ASTs that parsed
//   but do not represent correct expressions (eg, syntactically correct but otherwise
//   nonsensical operations)
// - Associativity matters; binary operations are left-associative so an operation on
//   the right may need parens even if it wouldn't if it were on the left.
//

type passCtx struct {
	parent ast.Node
}

func (*AddPlusObject) BaseContext(pass.ASTPass) pass.Context {
	return passCtx{}
}

// Visit replaces ApplyBrace with Binary node.
func (c *AddPlusObject) Visit(p pass.ASTPass, node *ast.Node, ctx pass.Context) {
	applyBrace, ok := (*node).(*ast.ApplyBrace)
	if ok {
		binary := &ast.Binary{
			NodeBase: applyBrace.NodeBase,
			Left:     applyBrace.Left,
			Op:       ast.BopPlus,
			Right:    applyBrace.Right,
		}
		*node = binary

		if c, ok := ctx.(passCtx); ok && c.parent != nil {
			needsParens := false
			switch parent := (c.parent).(type) {
			case *ast.ApplyBrace:
				panic("parent implicit-plus should already have been replaced with explicit plus")
			case *ast.Apply:
				// Apply binds tighter than ast.Binary, so we need parens here.
				if parent.Target == *node {
					needsParens = true
				}
				// We don't need parens if we're one of the arguments, since those are delimited.
			case *ast.Index:
				// Index binds tighter than ast.Binary, so we need parens here.
				if parent.Target == *node {
					needsParens = true
				}
				// We don't need parens in the context of node.Index, since that is delimited.
			case *ast.InSuper:
				// Check against precedence of `in` operator.
				// We can only be on the left, because InSuper RHS is always exactly `super`.
				needsParens = iast.BinaryOpPrecedence(ast.BopIn) <= plusPrec
			case *ast.Binary:
				// Check against precedence of the operator.
				opPrec := iast.BinaryOpPrecedence(parent.Op)
				if parent.Left == *node {
					needsParens = opPrec < plusPrec
				} else {
					needsParens = opPrec <= plusPrec
				}
			case *ast.Unary:
				// Check against precedence of the operator.
				needsParens = iast.UnaryPrecedence < plusPrec
			}

			if needsParens {
				*node = &ast.Parens{
					NodeBase: ast.NodeBase{
						Fodder:   binary.NodeBase.Fodder,
						LocRange: binary.NodeBase.LocRange,
					},
					Inner: *node,
				}
				binary.NodeBase.Fodder = nil
			}
		}
	}

	// Note we Visit starting from the replacement node, we don't go directly to its children.
	c.Base.Visit(p, node, passCtx{parent: *node})
}

var plusPrec = iast.BinaryOpPrecedence(ast.BopPlus)
