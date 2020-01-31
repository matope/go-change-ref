package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/imports"
)

type resultPresenter interface {
	writeContent(fname, content string) error
}

type result struct {
	fname, content string
}

type bufferedPresenter struct {
	buffer []result
}

func (p *bufferedPresenter) writeContent(fname, content string) error {
	p.buffer = append(p.buffer, result{fname: fname, content: content})
	return nil
}

func (p *bufferedPresenter) flush() error {
	for _, p := range p.buffer {
		if err := ioutil.WriteFile(p.fname, []byte(p.content), 0666); err != nil {
			return err
		}
	}
	return nil
}

type unbufferedPresenter struct{}

func (p *unbufferedPresenter) writeContent(fname, content string) error {
	fmt.Printf("file: %s", fname)
	_, err := fmt.Println(content)
	return err
}

type parameters struct {
	fromPkgPath, fromName string
	toPkgPath, toName     string
	toPkgName             string
	dir                   string
	resultPresenter       resultPresenter
	overlay               map[string][]byte
}

func main() {
	params, err := parseParamters()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("dir:%s\n", params.dir)
	fmt.Printf("from package: %q, symbol:%q\n", params.fromPkgPath, params.fromName)
	fmt.Printf("to   package: %q, symbol:%q", params.toPkgPath, params.toName)
	if params.toPkgName != "" {
		fmt.Printf(" (package name if duplication: %q)", params.toPkgName)
	}
	fmt.Printf("\n\n")

	if err := process(params); err != nil {
		log.Fatal(err)
	}
	if bp, ok := params.resultPresenter.(*bufferedPresenter); ok {
		bp.flush()
	}
}

func parseParamters() (*parameters, error) {
	flagFrom := flag.String("from", "", "from symbol. importpath.name")
	flagTo := flag.String("to", "", "to symbol. importpath.name.")
	flagToPkgName := flag.String("to-pkg-name", "", "package name used when the package name conflits with another imported package")
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

	var presenter resultPresenter
	if *flagOverwrite {
		presenter = &bufferedPresenter{}
	} else {
		presenter = &unbufferedPresenter{}
	}

	return &parameters{
		fromPkgPath:     fromPkgPath,
		fromName:        fromName,
		toPkgPath:       toPkgPath,
		toName:          toName,
		toPkgName:       *flagToPkgName,
		dir:             flag.Arg(0),
		resultPresenter: presenter,
	}, nil
}

func loadPkg(path string) (*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName,
	}

	pkgs, err := packages.Load(cfg, path)
	if err != nil {
		return nil, err
	}
	return pkgs[0], nil
}

func process(param *parameters) error {
	toPkg, err := loadPkg(param.toPkgPath)
	if err != nil {
		return err
	}

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedTypes |
			packages.LoadAllSyntax |
			packages.NeedTypesInfo,
		Tests:   true,
		Overlay: param.overlay,
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
		if err := processPackage(pkg, param, toPkg); err != nil {
			return err
		}
	}
	return nil
}

func processPackage(pkg *packages.Package, params *parameters, toPkg *packages.Package) error {
	fromLocal := pkg.PkgPath == params.fromPkgPath
	toLocal := pkg.PkgPath == params.toPkgPath
	//log.Printf("PkgPath:%s, PkgName:%s (fromLocal:%t, toLocal:%t)", pkg.PkgPath, pkg.Name, fromLocal, toLocal)

	var target types.Object
	if fromLocal {
		target = pkg.Types.Scope().Lookup(params.fromName)
		//if target == nil {
		//	return fmt.Errorf("pkgPath:%q name:%q not found locally.", params.fromPkgPath, params.fromName)
		//}
	} else {
		if scope := findImportScope(pkg.Types.Imports(), params.fromPkgPath); scope != nil {
			target = scope.Lookup(params.fromName)
		}
	}

	// records Idents that using target.
	targetIdents := findTargetIdents(pkg, target, fromLocal)
	usedIdents := findUsesDefsIdents(pkg)

	// setup nodeFilter.
	var nodeFilter func(node ast.Node) bool
	if fromLocal {

		// if from is local, find Ident nodes which are using target.
		if target != nil {
			nodeFilter = func(node ast.Node) bool {
				if ident, ok := node.(*ast.Ident); ok {
					_, ok := targetIdents[ident]
					return ok
				}
				return false
			}
		} else {
			var selectorIdents = make(map[*ast.Ident]struct{})
			nodeFilter = func(node ast.Node) bool {
				// mark selectorIdents.
				if selector, ok := node.(*ast.SelectorExpr); ok {
					selectorIdents[selector.Sel] = struct{}{}
					if xident, ok := selector.X.(*ast.Ident); ok {
						selectorIdents[xident] = struct{}{}
					}
				}

				if ident, ok := node.(*ast.Ident); ok {
					// if the ident was an Sel or X of a SelectorXpr already visited, skip it.
					if _, ok := selectorIdents[ident]; ok {
						return false
					}
					if _, ok := usedIdents[ident]; !ok && ident.Name == params.fromName {
						return true
					}
				}
				return false
			}
		}
	} else {
		if target != nil {
			nodeFilter = func(node ast.Node) bool {
				if selector, ok := node.(*ast.SelectorExpr); ok {
					_, ok := targetIdents[selector.Sel]
					return ok
				}
				return false
			}
		} else {
			pkgName := filepath.Base(params.fromPkgPath)
			// if from is not local, find selectorsnodes whose selector is using target.
			nodeFilter = func(node ast.Node) bool {
				if selector, ok := node.(*ast.SelectorExpr); ok {
					if _, ok := usedIdents[selector.Sel]; !ok {
						if xident, ok := selector.X.(*ast.Ident); ok && xident.Name == pkgName && selector.Sel.Name == params.fromName {
							//log.Printf("unused selector:%s, sel.sel.obj:%#v, sel.x:%#v", node, selector.Sel.Obj, selector.X)
							return true
						}
					}
					_, ok := targetIdents[selector.Sel]
					return ok
				}
				return false
			}
		}
	}

	for _, astFile := range pkg.Syntax {
		importMap := buildImportPathMap(pkg, astFile)
		//fmt.Println(importMap)
		// name -> import path
		inverted := make(map[string]string, len(importMap))
		for k, v := range importMap {
			inverted[v] = k
		}

		// toPkgName is used only when !toLocal.
		toPkgName := toPkg.Name
		if path, ok := inverted[toPkg.Name]; ok {
			if path != toPkg.PkgPath {
				// log.Printf("name:%s, path:%s, toPkg.PkgPath:%s", toPkg.Name, path, toPkg.PkgPath)
				// another package is already imported with the same name.
				// package name conflicted.
				if params.toPkgName == "" {
					return fmt.Errorf("%s: package name %q conflicted. please set -to-pkg-name.",
						pkg.Fset.Position(astFile.Pos()),
						toPkg.Name,
					)
				}
				toPkgName = params.toPkgName
			}
		}

		var replacedNode ast.Node
		if toLocal {
			replacedNode = &ast.Ident{Name: params.toName}
		} else {
			replacedNode = &ast.SelectorExpr{
				X:   &ast.Ident{Name: toPkgName},
				Sel: &ast.Ident{Name: params.toName},
			}
		}

		updated := false
		for i := range astFile.Decls {
			_ = astutil.Apply(astFile.Decls[i], func(cr *astutil.Cursor) bool {
				if nodeFilter(cr.Node()) {
					position := pkg.Fset.Position(cr.Node().Pos())
					line, _ := readaline(position)
					fmt.Printf("%s %s\n", position, line)
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
			if toPkgName != toPkg.Name {
				astutil.AddNamedImport(pkg.Fset, astFile, toPkgName, params.toPkgPath)
			} else {
				astutil.AddImport(pkg.Fset, astFile, params.toPkgPath)
			}
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
		if err := params.resultPresenter.writeContent(fname, string(b)); err != nil {
			return err
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

func findUsesDefsIdents(pkg *packages.Package) map[*ast.Ident]struct{} {
	var usedIdents = make(map[*ast.Ident]struct{})
	for ident := range pkg.TypesInfo.Uses {
		usedIdents[ident] = struct{}{}
	}
	for ident := range pkg.TypesInfo.Defs {
		usedIdents[ident] = struct{}{}
	}
	return usedIdents
}

func findTargetIdents(pkg *packages.Package, target types.Object, fromLocal bool) map[*ast.Ident]struct{} {
	var targetIdents = make(map[*ast.Ident]struct{})
	for ident, use := range pkg.TypesInfo.Uses {
		if use == target {
			targetIdents[ident] = struct{}{}
		}
	}

	// Do not overwrite receiver of method.
	// Here, attempting to delete idents which are receivers of methods.
	if fromLocal {
		for _, receiverIdent := range findReceiversOfMethodDecl(pkg) {
			delete(targetIdents, receiverIdent)
		}
	}

	return targetIdents
}

func findReceiversOfMethodDecl(pkg *packages.Package) []*ast.Ident {
	idents := make([]*ast.Ident, 0)
	for _, astFile := range pkg.Syntax {
		for _, decl := range astFile.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				if fd.Recv != nil {
					recvType := fd.Recv.List[0].Type
					if starExp, ok := recvType.(*ast.StarExpr); ok {
						recvType = starExp.X
					}
					if ident, ok := recvType.(*ast.Ident); ok {
						idents = append(idents, ident)
					}
				}
			}
		}
	}
	return idents
}

func buildImportPathMap(pkg *packages.Package, astFile *ast.File) map[string]string {
	m := make(map[string]string, len(astFile.Imports))
	for _, ispec := range astFile.Imports {
		path, err := strconv.Unquote(ispec.Path.Value)
		if err != nil {
			panic(err)
		}
		var pkgName string
		if ispec.Name != nil {
			pkgName = ispec.Name.String()
		} else {
			// If ispec doesn't have explicit name, we can query the implicit name
			// from Implicits.
			pkgName = pkg.TypesInfo.Implicits[ispec].(*types.PkgName).Name()
		}
		m[path] = pkgName
	}
	return m
}

func readaline(pos token.Position) (string, error) {
	f, err := os.Open(pos.Filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		if line == pos.Line {
			return scanner.Text(), nil
		}
	}
	return "", scanner.Err()
}
