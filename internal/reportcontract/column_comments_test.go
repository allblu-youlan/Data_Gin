package reportcontract

import (
	"strings"
	"testing"
)

func TestColumnCommentsDoNotChangePublishedContracts(t *testing.T) {
	version, parameters, columns, grants, arguments, result := validContract()
	contract := validSnapshotContract(t, version, result, columns)
	before, err := Compile(version, parameters, columns, grants, arguments, result, contract)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before.SpecJSON), `"Comment"`) {
		t.Fatal("column comments must not add fields to existing publication JSON")
	}
	result[0].Comment = "新的字段注释"
	after, err := Compile(version, parameters, columns, grants, arguments, result, contract)
	if err != nil || before.Hashes != after.Hashes || string(before.SpecJSON) != string(after.SpecJSON) {
		t.Fatalf("comments changed the publication contract: before=%+v after=%+v error=%v", before.Hashes, after.Hashes, err)
	}
	if err := VerifyRuntimeMetadata(before.SpecJSON, before.Hashes.Contract, before.Hashes.ProcedureSignature, before.Hashes.ResultSchema, arguments, result); err != nil {
		t.Fatalf("comments invalidated an existing publication contract: %v", err)
	}
}
