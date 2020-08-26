package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"strings"
	"text/template"

	"github.com/gostaticanalysis/analysisutil"
	"github.com/gostaticanalysis/astquery"
	"github.com/gostaticanalysis/comment"
	"github.com/gostaticanalysis/knife/knife"
	"golang.org/x/tools/go/packages"
)

var (
	flagFormat string
	flagXPath  string
)

func init() {
	flag.StringVar(&flagFormat, "f", "{{.}}", "output format")
	flag.StringVar(&flagXPath, "xpath", "", "A XPath expression for an AST node")
	flag.Parse()
}

func main() {
	cfg := &packages.Config{Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedDeps | packages.NeedTypesInfo}
	pkgs, err := packages.Load(cfg, flag.Args()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		os.Exit(1)
	}
	if packages.PrintErrors(pkgs) > 0 {
		os.Exit(1)
	}

	for _, pkg := range pkgs {
		pkg := pkg
		var cmaps comment.Maps
		tmpl, err := knife.Template.Funcs(template.FuncMap{
			"pos": func(v interface{}) token.Position {
				return knife.Position(pkg.Fset, v)
			},
			"objectof": func(s string) knife.Object {
				return objectOf(pkg.Types, s)
			},
			"typeof": func(s string) *knife.Type {
				return typeOf(pkg.Types, s)
			},
			"doc": func(v interface{}) string {
				node, ok := v.(interface{ Pos() token.Pos })
				if !ok {
					return ""
				}

				if cmaps == nil {
					cmaps = comment.New(pkg.Fset, pkg.Syntax)
				}

				pos := node.Pos()
				cgs := cmaps.CommentsByPosLine(pkg.Fset, pos)
				if len(cgs) > 0 {
					return strings.TrimSpace(cgs[len(cgs)-1].Text())
				}

				return ""
			},
		}).Parse(flagFormat)
		if err != nil {
			fmt.Fprintf(os.Stderr, "template parse error: %v\n", err)
			os.Exit(1)
		}
		var data interface{}
		if flagXPath != "" {
			e := astquery.New(pkg.Fset, pkg.Syntax, nil)
			v, err := e.Eval(flagXPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "XPath parse error: %v\n", err)
				os.Exit(1)
			}

			switch v := v.(type) {
			case []ast.Node:
				ns := make([]*knife.ASTNode, len(v))
				for i := range ns {
					ns[i] = knife.NewASTNode(pkg.TypesInfo, v[i])
				}
				data = ns
			default:
				data = v
			}

		} else {
			data = knife.NewPackage(pkg.Types)
		}

		if err := tmpl.Execute(os.Stdout, data); err != nil {
			fmt.Fprintf(os.Stderr, "template execute: %v\n", err)
			os.Exit(1)
		}
		fmt.Println()
	}
}

func objectOf(typesPkg *types.Package, s string) knife.Object {
	ss := strings.Split(s, ".")

	switch len(ss) {
	case 1:
		obj := types.Universe.Lookup(s)
		return knife.NewObject(obj)
	case 2:
		pkg, name := ss[0], ss[1]
		obj := analysisutil.LookupFromImports(typesPkg.Imports(), pkg, name)
		if obj != nil {
			return knife.NewObject(obj)
		}
		if analysisutil.RemoveVendor(typesPkg.Name()) != analysisutil.RemoveVendor(pkg) {
			return nil
		}
		return knife.NewObject(typesPkg.Scope().Lookup(name))
	}
	return nil
}

func typeOf(typesPkg *types.Package, s string) *knife.Type {
	if s == "" {
		return nil
	}

	if s[0] == '*' {
		typ := typeOf(typesPkg, s[1:])
		if typ == nil {
			return nil
		}
		return knife.NewType(types.NewPointer(typ.TypesType))
	}

	obj := objectOf(typesPkg, s)
	if obj == nil {
		return nil
	}

	return knife.NewType(obj.TypesObject().Type())
}
