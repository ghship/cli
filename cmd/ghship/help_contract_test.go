package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// Derive the documentation inventory from production code, not from help text.
// This only checks discoverability; behavioral regression lives in the CLI tests.
func TestImplementedCommandsHaveHelp(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range f.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "run" {
			continue
		}
		for _, statement := range fn.Body.List {
			switcher, ok := statement.(*ast.SwitchStmt)
			if !ok {
				continue
			}
			for _, statement := range switcher.Body.List {
				branch := statement.(*ast.CaseClause)
				for _, expression := range branch.List {
					literal, ok := expression.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					command, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatal(err)
					}
					if command == "help" || strings.HasPrefix(command, "-") {
						continue
					}
					var output bytes.Buffer
					if err := run([]string{"help", command}, nil, &output); err != nil || output.Len() == 0 {
						t.Fatalf("implemented command %s has no help: %v", command, err)
					}
					if !strings.Contains(help, command) {
						t.Errorf("root help omits %s", command)
					}
				}
			}
		}
	}
}

func TestImplementedFlagsAreDiscoverable(t *testing.T) {
	files := map[string]string{
		"auth.go": userHelp, "user.go": userHelp, "init.go": initHelp,
		"project.go": projectHelp, "doctor.go": doctorHelp,
		"install.go": installHelp, "lifecycle_entry.go": installHelp,
		"main.go": help + ghHelp + nextHelp + tidyHelp,
	}
	for name, documentation := range files {
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		check := func(value string) {
			if !strings.Contains(documentation, value) {
				t.Errorf("%s implements undocumented option %s", name, value)
			}
		}
		ast.Inspect(f, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.CallExpr:
				selector, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || len(n.Args) == 0 {
					break
				}
				receiver, ok := selector.X.(*ast.Ident)
				if !ok || receiver.Name != "f" {
					break
				}
				if selector.Sel.Name != "String" && selector.Sel.Name != "Bool" && selector.Sel.Name != "Int" {
					break
				}
				literal, ok := n.Args[0].(*ast.BasicLit)
				if !ok {
					break
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				check("--" + value)
			case *ast.CaseClause:
				for _, expression := range n.List {
					literal, ok := expression.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					value, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatal(err)
					}
					if strings.HasPrefix(value, "--") && value != "--help" && value != "--version" {
						check(value)
					}
				}
			}
			return true
		})
	}
}
