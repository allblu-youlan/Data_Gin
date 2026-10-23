package reportoracle

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/godror/godror"
)

func TestOperationProcedureBindsInputsAndOutputsWithoutInterpolatingValues(t *testing.T) {
	unsafeValue := "x'); DELETE FROM ORDERS; --"
	statement, arguments, outputs, err := buildOperationProcedureCall(ProcedureRef{Owner: "demo", Package: "pkg", Name: "sync"}, []ProcedureValue{{Name: "P_CODE", Direction: "IN", DataType: "VARCHAR2", Value: unsafeValue}, {Name: "P_COUNT", Direction: "IN/OUT", DataType: "NUMBER", Value: "9007199254740993"}, {Name: "R_FLAG", Direction: "OUT", DataType: "VARCHAR2"}})
	if err != nil {
		t.Fatal(err)
	}
	if statement != "BEGIN DEMO.PKG.SYNC(P_CODE => :arg_1, P_COUNT => :arg_2, R_FLAG => :arg_3); END;" || strings.Contains(statement, unsafeValue) {
		t.Fatalf("unsafe statement: %s", statement)
	}
	if arguments[0].(sql.NamedArg).Value != unsafeValue {
		t.Fatalf("input changed: %+v", arguments[0])
	}
	inout := arguments[1].(sql.NamedArg).Value.(sql.Out)
	if !inout.In || *inout.Dest.(*godror.Number) != godror.Number("9007199254740993") {
		t.Fatalf("IN OUT binding=%+v", inout)
	}
	*inout.Dest.(*godror.Number) = godror.Number("9007199254740994")
	flag := arguments[2].(sql.NamedArg).Value.(sql.Out)
	*flag.Dest.(*string) = "执行成功"
	if outputs["P_COUNT"].value() != "9007199254740994" || outputs["R_FLAG"].value() != "执行成功" {
		t.Fatalf("outputs=%+v", outputs)
	}
}

func TestOperationValuesPreserveDecimalsAndBindDates(t *testing.T) {
	value, err := OperationValue("NUMBER", "12345678901234567890.123456")
	if err != nil || value != godror.Number("12345678901234567890.123456") {
		t.Fatalf("decimal=%v error=%v", value, err)
	}
	value, err = OperationValue("DATE", "2026-10-22")
	if err != nil || value.(time.Time).Format("2006-01-02") != "2026-10-22" {
		t.Fatalf("date=%v error=%v", value, err)
	}
	for _, test := range []struct{ name, dataType, value string }{{"invalid number", "NUMBER", "1 OR 1=1"}, {"invalid date", "DATE", "tomorrow"}, {"unsupported type", "SYS_REFCURSOR", ""}} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := OperationValue(test.dataType, test.value); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestOperationProcedureEmptyInputsBindNull(t *testing.T) {
	for _, kind := range []string{"VARCHAR2", "NUMBER", "DATE", "TIMESTAMP", "CLOB", "NCLOB"} {
		t.Run(kind, func(t *testing.T) {
			_, arguments, outputs, err := buildOperationProcedureCall(ProcedureRef{Owner: "DEMO", Name: "SYNC"}, []ProcedureValue{
				{Name: "P_VALUE", Direction: "IN", DataType: kind},
				{Name: "P_RESULT", Direction: "IN OUT", DataType: kind},
			})
			if err != nil {
				t.Fatal(err)
			}
			if value := arguments[0].(sql.NamedArg).Value; value != nil {
				t.Fatalf("empty %s input = %#v, want NULL", kind, value)
			}
			inout := arguments[1].(sql.NamedArg).Value.(sql.Out)
			if !inout.In || outputs["P_RESULT"].value() != "" {
				t.Fatalf("empty IN OUT binding = %+v", inout)
			}
		})
	}
}
