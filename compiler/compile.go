package compiler

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/wellington/sass/ast"
	"github.com/wellington/sass/parser"
	"github.com/wellington/sass/token"
)

type Context struct {
	buf      *bytes.Buffer
	fileName *ast.Ident
	// Records the current level of selectors
	// Each time a selector is encountered, increase
	// by one. Each time a block is exited, remove
	// the last selector
	sels      []string
	firstRule bool
	level     int
	printers  map[ast.Node]func(*Context, ast.Node)

	typ Scope
}

// stores types and values with scoping. To remove a scope
// use CloseScope(), to open a new Scope use OpenScope().
type Scope interface {
	// OpenScope() Typ
	// CloseScope() Typ
	Get(string) interface{}
	Set(string, interface{})
	// Number of Rules in this scope
	RuleAdd(*ast.RuleSpec)
	RuleLen() int
}

var (
	empty = new(emptyTyp)
)

type emptyTyp struct{}

func (*emptyTyp) Get(name string) interface{} {
	return nil
}

func (*emptyTyp) Set(name string, _ interface{}) {}

func (*emptyTyp) RuleLen() int { return 0 }

func (*emptyTyp) RuleAdd(*ast.RuleSpec) {}

type valueScope struct {
	Scope
	rules []*ast.RuleSpec
	m     map[string]interface{}
}

func (t *valueScope) RuleAdd(rule *ast.RuleSpec) {
	t.rules = append(t.rules, rule)
}

func (t *valueScope) RuleLen() int {
	return len(t.rules)
}

func (t *valueScope) Get(name string) interface{} {
	val, ok := t.m[name]
	if ok {
		return val
	}
	return t.Scope.Get(name)
}

func (t *valueScope) Set(name string, v interface{} /* should this just be string? */) {
	t.m[name] = v
}

func NewTyp() Scope {
	return &valueScope{Scope: empty, m: make(map[string]interface{})}
}

func NewScope(s Scope) Scope {
	return &valueScope{Scope: s, m: make(map[string]interface{})}
}

func CloseScope(typ Scope) Scope {
	s, ok := typ.(*valueScope)
	if !ok {
		return typ
	}
	return s.Scope
}

func fileRun(path string) (string, error) {
	ctx := &Context{}
	ctx.Init()
	out, err := ctx.Run(path)
	if err != nil {
		log.Fatal(err)
	}
	return out, err
}

// Run takes a single Sass file and compiles it
func (ctx *Context) Run(path string) (string, error) {
	// func ParseFile(fset *token.FileSet, filename string, src interface{}, mode Mode) (f *ast.File, err error) {
	fset := token.NewFileSet()
	pf, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.Trace)
	if err != nil {
		return "", err
	}

	ast.Walk(ctx, pf)
	lr, _ := utf8.DecodeLastRune(ctx.buf.Bytes())
	_ = lr
	if ctx.buf.Len() > 0 && lr != '\n' {
		ctx.out("\n")
	}
	// ctx.printSels(pf.Decls)
	return ctx.buf.String(), nil
}

// out prints with the appropriate indention, selectors always have indent
// 0
func (ctx *Context) out(v string) {
	fr, _ := utf8.DecodeRuneInString(v)
	if fr == '\n' {
		fmt.Fprintf(ctx.buf, v)
		return
	}
	ws := []byte("                                              ")
	format := append(ws[:ctx.level*2], "%s"...)
	fmt.Fprintf(ctx.buf, string(format), v)
}

func (ctx *Context) blockIntro() {

	ctx.firstRule = false
	if ctx.buf.Len() > 0 && ctx.level == 0 {
		ctx.out("\n\n")
	}

	// Will probably need better logic around this
	sels := strings.Join(ctx.sels, " ")
	ctx.out(fmt.Sprintf("%s {\n", sels))
}

func (ctx *Context) blockOutro() {
	var skipParen bool
	if len(ctx.sels) > 0 {
		ctx.sels = ctx.sels[:len(ctx.sels)-1]
	}
	if ctx.firstRule {
		return
	}

	ctx.firstRule = true
	// if len(ctx.sels) != ctx.level {
	// 	panic(fmt.Sprintf("level mismatch lvl:%d sels:%d",
	// 		ctx.level,
	// 		len(ctx.sels)))
	// }
	if !skipParen {
		fmt.Fprintf(ctx.buf, " }")
		// ctx.out(" }")
	}
	// fmt.Fprintf(ctx.buf, " }")
}

func (ctx *Context) Visit(node ast.Node) ast.Visitor {
	switch v := node.(type) {
	case *ast.BlockStmt:
		if ctx.typ.RuleLen() > 0 {
			ctx.level = ctx.level + 1

			// fmt.Println("closing because of", ctx.typ.(*valueScope).rules)
			// Close the previous spec if any rules exist in it
			fmt.Fprintf(ctx.buf, " }\n")
		}
		ctx.typ = NewScope(ctx.typ)
		ctx.firstRule = true
		for _, node := range v.List {
			ast.Walk(ctx, node)
		}
		if ctx.level > 0 {
			ctx.level = ctx.level - 1
		}
		ctx.typ = CloseScope(ctx.typ)
		ctx.blockOutro()
		ctx.firstRule = true
		// ast.Walk(ctx, v.List)
		// fmt.Fprintf(ctx.buf, "}")
		return nil
	case *ast.SelDecl:
		ctx.printers[selDecl](ctx, v)
	case *ast.File:
		// Nothing to print for these
	case *ast.GenDecl:

	case *ast.Ident:
		// The first IDENT is always the filename, just preserve
		// it somewhere
		if ctx.fileName == nil {
			ctx.fileName = ident
			return ctx
		}
		ctx.printers[ident](ctx, v)
	case *ast.PropValueSpec:
		ctx.printers[propSpec](ctx, v)
	case *ast.DeclStmt:
		ctx.printers[declStmt](ctx, v)
	case *ast.ValueSpec:
		ctx.printers[valueSpec](ctx, v)
	case *ast.RuleSpec:
		ctx.printers[ruleSpec](ctx, v)
	case *ast.SelStmt:
		// We will need to combine parent selectors
		// while printing these
		ctx.printers[selStmt](ctx, v)
		// Nothing to do
	case *ast.BasicLit:
		ctx.printers[expr](ctx, v)
	case nil:

	default:
		fmt.Printf("add printer for: %T\n", v)
		fmt.Printf("% #v\n", v)
	}
	return ctx
}

var (
	ident     *ast.Ident
	expr      ast.Expr
	declStmt  *ast.DeclStmt
	valueSpec *ast.ValueSpec
	ruleSpec  *ast.RuleSpec
	selDecl   *ast.SelDecl
	selStmt   *ast.SelStmt
	propSpec  *ast.PropValueSpec
	typeSpec  *ast.TypeSpec
)

func (ctx *Context) Init() {
	ctx.buf = bytes.NewBuffer(nil)
	ctx.printers = make(map[ast.Node]func(*Context, ast.Node))
	ctx.printers[valueSpec] = visitValueSpec

	ctx.printers[ident] = printIdent
	ctx.printers[declStmt] = printDecl
	ctx.printers[ruleSpec] = printRuleSpec
	ctx.printers[selDecl] = printSelDecl
	ctx.printers[selStmt] = printSelStmt
	ctx.printers[propSpec] = printPropValueSpec
	ctx.printers[expr] = printExpr
	ctx.typ = NewScope(empty)
	// ctx.printers[typeSpec] = visitTypeSpec
	// assign printers
}

func printExpr(ctx *Context, n ast.Node) {
	switch v := n.(type) {
	case *ast.BasicLit:
		return
		fmt.Println("basic lit", v.Value)
		ctx.out(v.Value)
	}
}

func printSelStmt(ctx *Context, n ast.Node) {
	stmt := n.(*ast.SelStmt)
	ctx.sels = append(ctx.sels, stmt.Name.String())
}

func printSelDecl(ctx *Context, n ast.Node) {
	decl := n.(*ast.SelDecl)
	ctx.sels = append(ctx.sels, decl.Name.String())
}

func printRuleSpec(ctx *Context, n ast.Node) {
	// Inspect the sel buffer and dump it
	// We'll also need to track what level was last dumped
	// so selectors don't get printed twice
	if ctx.firstRule {
		ctx.blockIntro()
	} else {
		ctx.out("\n")
	}
	spec := n.(*ast.RuleSpec)
	ctx.typ.RuleAdd(spec)
	ctx.out(fmt.Sprintf("  %s: ", spec.Name))
}

func printPropValueSpec(ctx *Context, n ast.Node) {
	spec := n.(*ast.PropValueSpec)
	fmt.Fprintf(ctx.buf, spec.Name.String()+";")
}

// Variable declarations
func visitValueSpec(ctx *Context, n ast.Node) {
	spec := n.(*ast.ValueSpec)

	names := make([]string, len(spec.Names))
	for i, nm := range spec.Names {
		names[i] = nm.Name
	}

	if len(spec.Values) > 0 {
		ctx.typ.Set(names[0], simplifyExprs(ctx, spec.Values))
	} else {
		ctx.out(fmt.Sprintf("%s;", ctx.typ.Get(names[0])))
	}
	// ctx.out(fmt.Sprintf("%s;", strings.Join(names, " ")))
}

func simplifyExprs(ctx *Context, exprs []ast.Expr) string {
	var sums []string
	for _, expr := range exprs {
		// fmt.Printf("expr: % #v\n", expr)
		switch v := expr.(type) {
		case *ast.Ident:
			if v.Obj == nil {
				sums = append(sums, v.Name)
				continue
			}
			switch v.Obj.Kind {
			case ast.Var:
				s, ok := ctx.typ.Get(v.Obj.Name).(string)
				if ok {
					sums = append(sums, s)
				}
			default:
				fmt.Println("unsupported obj kind")
			}
		case *ast.BasicLit:
			sums = append(sums, v.Value)
		default:
			log.Fatalf("unhandled expr: % #v\n", v)
		}
	}
	return strings.Join(sums, " ")
}

func printDecl(ctx *Context, ident ast.Node) {
	// I think... nothing to print we'll see
}

func printIdent(ctx *Context, ident ast.Node) {
	ctx.out(ident.(*ast.Ident).String())
}
