package ast

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/wellington/sass/token"
)

var (
	regEql = regexp.MustCompile("\\s*(\\*?=)\\s*").ReplaceAll
	regBkt = regexp.MustCompile("\\s*(\\[)\\s*(\\S+)\\s*(\\])").ReplaceAll
)

// Resolves walks selector operations removing nested Op by prepending X
// on Y.
func (stmt *SelStmt) Resolve(fset *token.FileSet) {

	s := &sel{
		parent: stmt.Parent,
		stmt:   stmt,
		prec:   token.LowestPrec + 1,
		parts:  make(map[token.Pos]*BasicLit),
	}
	Print(fset, s.stmt.Sel)
	// This could be more efficient, it should inspect precision of
	// the top node
	for prec := token.UnaryPrec; prec > 1; prec-- {
		// Walk the selectors resolving ops found at the active
		// precision
		if s.parent != nil {
			s.inject = true
		}
		s.prec = prec
		Walk(s, s.stmt.Sel)
	}

	// stmt.Resolved = stmt.Sel.(*BasicLit)
	Print(fset, s.stmt.Sel)
	fmt.Println("parts len", len(s.parts))
	var vals []string
	for i, part := range s.parts {
		fmt.Printf("%d: % #v\n", i, part)
		vals = append(vals, part.Value)
	}
	val := strings.Join(vals, " ")
	stmt.Resolved = &BasicLit{Value: val}

}

type sel struct {
	stmt   *SelStmt
	parent *SelStmt
	parts  map[token.Pos]*BasicLit
	prec   int    // Resolve each precendence in order
	stack  []Expr // Nesting stack
	inject bool   // inject parent to start
}

func (s *sel) add(pos token.Pos, lit *BasicLit) {
	s.parts[pos] = lit
	// FIXME: walk through all available positions and remove
	// any higher than pos. This indicates a reduce happened
	// and something was reported prematurely
	for i := range s.parts {
		if i > pos {
			delete(s.parts, i)
		}
	}
}

func (s *sel) Visit(node Node) Visitor {
	var pos token.Pos
	var add *BasicLit
	defer func() {
		if add != nil && add.Kind != token.ILLEGAL {
			s.add(pos, add)
			// s.parts = append(s.parts, add)
			fmt.Printf("adding %d: % #v\n", pos, add)
		}
	}()
	// fmt.Printf("%d: (%p) % #v\n", s.prec, node, node)
	switch v := node.(type) {
	case *UnaryExpr:
		fmt.Println("unary")
		// Nesting, collapse &
		if v.Visited {
			return nil
		}
		if s.prec < 5 {
			panic(fmt.Errorf("invalid nest token: %s prec: %d", v.Op, s.prec))
		}
		if s.prec != 5 {
			return nil
		}
		s.inject = false
		v.Visited = true
		x := s.switchExpr(v.X)
		x.ValuePos = v.Pos()
		_ = x
		pos = x.Pos()
		add = x
		return nil
	case *BasicLit:
		if v.Kind == token.ILLEGAL {
			return nil
		}
		if s.prec != 2 {
			return nil
		}
		delim := " "
		var val = v.Value
		fmt.Printf("prec %d inject? %t\n", s.prec, s.inject)
		if s.inject && s.parent != nil {
			val = s.parent.Resolved.Value + delim + v.Value
		}
		v.Value = val
		add = v
		return nil
	case *BinaryExpr:
		pos = v.Pos()
		fmt.Printf("binary %d % #v\n", v.Pos(), v)
		switch v.Op {
		case token.NEST:
			if s.prec < 5 {
				panic(fmt.Errorf("invalid binary nest token: %s prec: %d", v.Op, s.prec))
			}
			if s.prec != 5 {
				return s
			}
		case token.ADD, token.GTR, token.TIL:
			if s.prec < 4 {
				return nil
				panic(fmt.Errorf("invalid Op token: %s prec: %d", v.Op, s.prec))
			}
			if s.prec != 4 {
				return s
			}
			add = s.joinBinary(v)
		case token.COMMA:
			if s.prec < 3 {
				return nil
				panic(fmt.Errorf("invalid group token: %s prec: %d", v.Op, s.prec))
			}
			if s.prec != 3 {
				// Reset parent injector
				s.inject = true
				Walk(s, v.X)
				// Reset parent injector
				s.inject = true
				Walk(s, v.Y)
				return nil
			}
			add = s.joinBinary(v)
		}

		// v.Op = token.ILLEGAL
		return nil
	}

	return s
}

func (s *sel) switchExpr(expr Expr) *BasicLit {
	switch v := expr.(type) {
	case *BasicLit:
		// v.Kind = token.ILLEGAL
		return v
	case *UnaryExpr:
		return s.switchExpr(v.X)
	case *BinaryExpr:
		return s.joinBinary(v)
	default:
		panic(fmt.Errorf("switch expr: % #v\n", v))
	}
}

func (s *sel) joinBinary(bin *BinaryExpr) *BasicLit {
	var x, y *BasicLit
	x = s.switchExpr(bin.X)
	y = s.switchExpr(bin.Y)

	delim := " " // This will change with compiler mode

	var val string
	if bin.Op == token.COMMA {
		val = x.Value + bin.Op.String() + delim + y.Value
	} else {
		vals := []string{x.Value, bin.Op.String(), y.Value}
		val = strings.Join(vals, delim)
	}

	lit := &BasicLit{
		ValuePos: bin.Pos(),
		Value:    val,
		Kind:     token.STRING,
	}
	fmt.Printf("joinBin ret %s\n", val)
	return lit
}
