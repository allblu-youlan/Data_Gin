package tableboard

import (
	"strings"
	"testing"
)

func TestExpressionPlan(t *testing.T) {
	expr := func(f string) Cell { return Cell{Kind: "expression", Expression: f} }
	s := Section{Cells: [][]Cell{{{}, {}, expr("=SUM(A1:$B$1,A1*2)")}, {expr("=IFERROR(C1/(B1-4),\"-\")"), expr("=-($C$1+A1)*2/4"), expr("=SUM(A2,B2)")}}}
	plan, e := CompileExpressions(s)
	if e != nil {
		t.Fatal(e)
	}
	external := map[Position]ExpressionValue{{0, 0}: {Status: "ok", Value: "1.25"}, {0, 1}: {Status: "ok", Value: "4"}}
	compute := func() map[Position]ExpressionValue {
		return plan.Evaluate(func(p Position) ExpressionValue { return external[p] })
	}
	out := compute()
	for p, want := range map[Position]string{{0, 2}: "7.75", {1, 0}: "-", {1, 1}: "-4.5", {1, 2}: "-4.5"} {
		if v := out[p]; v.Status != "ok" || v.Value != want {
			t.Fatal(p, v, want)
		}
	}
	external[Position{0, 1}] = ExpressionValue{Status: "error", Message: "权限已撤销"}
	out = compute()
	if out[Position{1, 0}].Status != "error" || out[Position{1, 0}].Message != "权限已撤销" {
		t.Fatal(out)
	}
	external[Position{0, 1}] = ExpressionValue{Status: "null", Message: "全部为空"}
	if v := compute()[Position{1, 0}]; v.Status != "null" {
		t.Fatal(v)
	}
}
func TestExpressionRejectsInvalidReferencesAndUnsupportedSyntax(t *testing.T) {
	for _, f := range []string{"=A1+1", "=B1*2", "=C1", "=SUM(B1:A1)", "=exec(B1)", "=SUM(B1);x", "=OTHER!B1", "=IFERROR(1/0,12)", "=A0", strings.Repeat("1", 2049)} {
		s := Section{Cells: [][]Cell{{{Kind: "expression", Expression: f}, {Kind: "expression", Expression: "=A1"}}}}
		if _, e := CompileExpressions(s); e == nil {
			t.Fatal("accepted", f)
		}
	}
}
