package wasm

import (
	"context"
	"testing"
)

// Minimal WASM module that exports an "add" function: (i32, i32) -> i32
// Built from WAT:
//
//	(module
//	  (func (export "add") (param i32 i32) (result i32)
//	    local.get 0
//	    local.get 1
//	    i32.add))
var addWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f, // type section
	0x03, 0x02, 0x01, 0x00, // function section
	0x07, 0x07, 0x01, 0x03, 0x61, 0x64, 0x64, 0x00, 0x00, // export section
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b, // code section
}

func TestPluginHost_LoadAndCall(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer host.Close(ctx)

	// Load the add module
	err := host.LoadPlugin(ctx, "math", addWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Call add(2, 3)
	result, err := host.CallFunction(ctx, "math", "add", 2, 3)
	if err != nil {
		t.Fatalf("CallFunction failed: %v", err)
	}

	if len(result) != 1 || result[0] != 5 {
		t.Errorf("Expected [5], got %v", result)
	}
}

func TestPluginHost_LoadInvalidWASM(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer host.Close(ctx)

	err := host.LoadPlugin(ctx, "invalid", []byte{0x00, 0x01, 0x02, 0x03})
	if err == nil {
		t.Error("Expected error for invalid WASM")
	}
}
