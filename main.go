package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/printer"
	"go/types"
	"io/ioutil"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/imports"
)

type parameters struct {
	fromPkgPath, fromName string
	toPkgPath, toName     string
	dir                   string
	overwrite             bool
}

func main() {
	param, err := parseParamters()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Parameter:%+v\n", param)

	if err := process(param); err != nil {
		log.Fatal(err)
	}
}

func parseParamters() (*parameters, error) {
	flagFrom := flag.String("from", "", "from symbol. importpath.name")
	flagTo := flag.String("to", "", "to symbol. importpath.name.")
	flagOverwrite := flag.Bool("w", false, "overwrite .go code")
	flag.Parse()

	pos := strings.LastIndex(*flagFrom, ".")
	if pos < 0 {
		return nil, errors.New("-from does not contain '.'")
	}
	fromPkgPath, fromName := (*flagFrom)[:pos], (*flagFrom)[pos+1:]

	pos = strings.LastIndex(*flagTo, ".")
	if pos < 0 {
		return nil, errors.New("-to does not contain '.'")
	}
	toPkgPath, toName := (*flagTo)[:pos], (*flagTo)[pos+1:]

	return &parameters{
		fromPkgPath: fromPkgPath,
		fromName:    fromName,
		toPkgPath:   toPkgPath,
		toName:      toName,
		dir:         flag.Arg(0),
		overwrite:   *flagOverwrite,
	}, nil
}

func process(param *parameters) error {
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedTypes |
			packages.LoadAllSyntax |
			packages.NeedTypesInfo,
		Tests: true,
	}

	pkgs, err := packages.Load(cfg, param.dir)
	if err != nil {
		return err
	}

	// sort pkgs by name.
	sort.Slice(pkgs, func(i, j int) bool {
		return pkgs[i].String() < pkgs[j].String()
	})
	for _, pkg := range pkgs {
		if err := processPackage(pkg, param); err != nil {
			return err
		}
	}
	return nil
}

func processPackage(pkg *packages.Package, params *parameters) error {
	fromLocal := pkg.PkgPath == params.fromPkgPath
	toLocal := pkg.PkgPath == params.toPkgPath
	//log.Printf("PkgPath:%s, PkgName:%s (fromLocal:%t, toLocal:%t)", pkg.PkgPath, pkg.Name, fromLocal, toLocal)

	var target types.Object
	if fromLocal {
		target = pkg.Types.Scope().Lookup(params.fromName)
		if target == nil {
			return fmt.Errorf("pkgPath:%q name:%q not found locally.", params.fromPkgPath, params.fromName)
		}
	} else {
		scope := findImportScope(pkg.Types.Imports(), params.fromPkgPath)
		if scope == nil {
			return nil
		}
		target = scope.Lookup(params.fromName)
		if target == nil {
			return fmt.Errorf("name:%q not found in package:%q.", params.fromName, params.fromPkgPath)
		}
	}
	if target == nil {
		return nil
	}

	// records Idents that using target.
	var usedIdents = make(map[*ast.Ident]struct{})
	for ident, use := range pkg.TypesInfo.Uses {
		if use == target {
			//log.Printf("%s: ident:%q is using %q\n", pkg.Fset.Position(ident.Pos()).String(), ident.String(), target.String())
			usedIdents[ident] = struct{}{}
		}
	}

	// Do not overwrite receiver of method.
	// Here, attempting to delete idents which are receivers of methods.
	if fromLocal {
		for _, astFile := range pkg.Syntax {
			for _, decl := range astFile.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok {
					if fd.Recv != nil {
						recvType := fd.Recv.List[0].Type
						if starExp, ok := recvType.(*ast.StarExpr); ok {
							recvType = starExp.X
						}
						if ident, ok := recvType.(*ast.Ident); ok {
							delete(usedIdents, ident)
						}
					}
				}
			}
		}
	}

	var nodeFilter func(cr *astutil.Cursor) bool
	if fromLocal {
		// if from is local, find Ident nodes which are using target.
		nodeFilter = func(cr *astutil.Cursor) bool {
			if ident, ok := cr.Node().(*ast.Ident); ok {
				if _, ok := usedIdents[ident]; !ok {
					return false
				}
				if fd, ok := cr.Parent().(*ast.FuncDecl); ok {
					return fd.Recv == nil // if the Ident is
				}
				return true
			}
			return false
		}
	} else {
		// if from is not local, find selectorsnodes whose selector is using target.
		nodeFilter = func(cr *astutil.Cursor) bool {
			if selector, ok := cr.Node().(*ast.SelectorExpr); ok {
				_, ok := usedIdents[selector.Sel]
				return ok
			}
			return false
		}
	}

	var replacedNode ast.Node
	if toLocal {
		replacedNode = &ast.Ident{Name: params.toName}
	} else {
		replacedNode = &ast.SelectorExpr{
			X:   &ast.Ident{Name: filepath.Base(params.toPkgPath)},
			Sel: &ast.Ident{Name: params.toName},
		}
	}

	for _, astFile := range pkg.Syntax {
		updated := false
		for i := range astFile.Decls {
			_ = astutil.Apply(astFile.Decls[i], func(cr *astutil.Cursor) bool {
				if nodeFilter(cr) {
					//log.Printf("%s: ident:%q is using %q\n", pkg.Fset.Position(ident.Pos()).String(), ident.String(), target.Name())
					log.Printf("found:%q", pkg.Fset.Position(cr.Node().Pos()))
					cr.Replace(replacedNode)
					updated = true
				}
				return true
			}, nil)
		}

		if !updated {
			continue
		}

		if !toLocal {
			astutil.AddImport(pkg.Fset, astFile, params.toPkgPath)
		}

		buf := &bytes.Buffer{}
		if err := printer.Fprint(buf, pkg.Fset, astFile); err != nil {
			return err
		}

		// Remove unused imports if any.
		b, err := imports.Process("", buf.Bytes(), nil)
		if err != nil {
			return err
		}

		fname := pkg.Fset.Position(astFile.Pos()).Filename
		fmt.Println("filename:", fname)

		if params.overwrite {
			ioutil.WriteFile(fname, b, 0666)
		} else {
			fmt.Printf("%s", b)
		}
	}
	return nil
}

func findImportScope(impts []*types.Package, pkgPath string) *types.Scope {
	for _, impt := range impts {
		if impt.Path() == pkgPath {
			return impt.Scope()
		}
	}
	return nil
}
