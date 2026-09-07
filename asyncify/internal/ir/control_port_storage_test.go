package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestControlPortStorageNamesRootAndReusedSiblingPorts(t *testing.T) {
	values, lowered := lowerValueFixture(t, `(module
  (func (result i32)
   block (result i32) i32.const 7 end drop
   block (result i32) i32.const 9 br 1 end))`)
	root, ok := values.Scope(0)
	if !ok || root.Kind() != FunctionScope || root.ResultCount() != 1 {
		t.Fatal("missing source function result scope")
	}
	rootID := root.ResultValue(0)
	rootLocal, ok := lowered.PortStorage(rootID)
	if !ok {
		t.Fatal("root branch lost its materialized result port")
	}
	var siblingIDs []ValueID
	var siblingLocals []uint32
	for label, metadata := range values.scopes {
		if metadata.kind != BlockScope {
			continue
		}
		scope, exists := values.Scope(label)
		if !exists {
			t.Fatalf("missing scope %d", label)
		}
		id := scope.ResultValue(0)
		local, assigned := lowered.PortStorage(id)
		if !assigned {
			t.Fatal("scope port not assigned")
		}
		siblingIDs = append(siblingIDs, id)
		siblingLocals = append(siblingLocals, local)
	}
	if len(siblingIDs) != 2 {
		t.Fatalf("expected two sibling scopes, got %d", len(siblingIDs))
	}
	if siblingIDs[0] == siblingIDs[1] || siblingIDs[0] == rootID || siblingIDs[1] == rootID {
		t.Fatal("distinct source ports share identity")
	}
	if siblingLocals[0] != siblingLocals[1] {
		t.Fatal("completed siblings no longer reuse carrier")
	}
	if siblingLocals[0] == rootLocal {
		t.Fatal("active root port aliases child carrier")
	}
	if _, assigned := lowered.PortStorage(0); assigned {
		t.Fatal("unknown source value acquired storage")
	}
}

func TestControlPortStorageRejectsIncorrectAssignment(t *testing.T) {
	for _, scenario := range []string{"arity", "unassigned", "type"} {
		t.Run(scenario, func(t *testing.T) {
			values, err := valueFixture(t, `(module (func (result i32) block (result i32) i32.const 7 end))`)
			if err != nil {
				t.Fatal(err)
			}
			next := uint32(20)
			storage, err := planControlStorage(values.control, func(wasm.ValType) uint32 { local := next; next++; return local }, true)
			if err != nil {
				t.Fatal(err)
			}
			scope := values.scopes[1]
			carriers := storage.nodes[scope.owner]
			local := carriers.results[0]
			switch scenario {
			case "arity":
				carriers.results = nil
				storage.nodes[scope.owner] = carriers
			case "unassigned":
				delete(storage.types, local)
			case "type":
				storage.types[local] = wasm.ValI64
			}
			if err := storage.bindSourcePorts(values); err == nil {
				t.Fatal("accepted invalid source-port assignment")
			}
			if storage.portLocals != nil {
				t.Fatal("published partial source-port bindings")
			}
		})
	}
}

func TestControlPortStorageRejectsForeignFunctionWrapper(t *testing.T) {
	values, err := valueFixture(t, `(module (func (result i32) i32.const 7 br 0))`)
	if err != nil {
		t.Fatal(err)
	}
	next := uint32(20)
	storage, err := planControlStorage(values.control, func(wasm.ValType) uint32 { local := next; next++; return local }, true)
	if err != nil {
		t.Fatal(err)
	}
	original := storage.root.(*BlockNode)
	foreign := *original
	foreign.Body = &SeqNode{}
	storage.nodes[&foreign] = storage.nodes[original]
	storage.root = &foreign
	if err := storage.bindSourcePorts(values); err == nil {
		t.Fatal("foreign same-shaped wrapper acquired source function ports")
	}
}
