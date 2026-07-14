package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestBinaryE2EParallelSafety(t *testing.T) {
	t.Parallel()
	files := []string{
		"app_e2e_test.go",
		"app_skills_e2e_test.go",
		"app_workflows_e2e_test.go",
	}
	const serialTest = "TestRunCanonicalSkillWorkflowDelegatesThroughLocalBinEndToEnd"

	for _, path := range files {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv != nil || function.Body == nil || len(function.Name.Name) < 4 || function.Name.Name[:4] != "Test" {
					continue
				}
				usesParallelBinary := callsFunction(function.Body, "buildZVBinary")
				runsInParallel := usesParallelBinary || callsSelector(function.Body, "t", "Parallel")
				mutatesProcessState := callsFunction(function.Body, "withWorkingDir") ||
					callsSelector(function.Body, "os", "Chdir") ||
					callsSelector(function.Body, "os", "Setenv") ||
					callsSelector(function.Body, "os", "Unsetenv") ||
					callsSelector(function.Body, "t", "Setenv")
				if function.Name.Name == serialTest {
					if runsInParallel {
						t.Errorf("%s must stay serial", function.Name.Name)
					}
					continue
				}
				if !runsInParallel {
					t.Errorf("%s does not opt into parallel execution", function.Name.Name)
				}
				if mutatesProcessState {
					t.Errorf("%s mutates process-wide state but runs in parallel", function.Name.Name)
				}
			}
		})
	}
}

func callsFunction(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if ok && identifier.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

func callsSelector(node ast.Node, receiver, method string) bool {
	found := false
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != method {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && identifier.Name == receiver {
			found = true
			return false
		}
		return true
	})
	return found
}
