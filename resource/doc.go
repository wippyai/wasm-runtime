// Package resource provides Component Model resource handle management.
//
// Resources are opaque handles representing host-side values that can be
// passed to and from WASM components. This package implements the handle
// table required by the Component Model's resource lifecycle.
//
// # Resource Lifecycle
//
// The Component Model defines three resource operations:
//
//	own<T>    - Ownership transfer (caller loses handle)
//	borrow<T> - Temporary access (handle remains valid)
//	drop      - Explicit destruction of owned resource
//
// # Handle Table
//
// The UnifiedTable maps integer handles to Go values:
//
//	table := resource.NewTable()
//
//	// Insert a value, get a handle
//	handle := table.Insert(typeID, myValue)
//
//	// Retrieve value by handle
//	value, ok := table.Get(handle)
//
//	// Remove and get value (for ownership transfer)
//	value, ok := table.Remove(handle)
//
// # Type Safety
//
// Handles are typed - each resource type gets a unique type ID:
//
//	const FileTypeID = 1
//	const SocketTypeID = 2
//
//	// Insert with type
//	fileHandle := table.Insert(FileTypeID, file)
//
//	// Type-checked retrieval
//	value, ok := table.GetTyped(fileHandle, FileTypeID) // ok
//	value, ok := table.GetTyped(fileHandle, SocketTypeID) // !ok
//
// # Observers
//
// Register observers to track resource lifecycle events:
//
//	table.AddObserver(func(event resource.Event) {
//	    switch event.Type {
//	    case resource.EventCreated:
//	        log.Printf("resource %d created", event.Handle)
//	    case resource.EventDropped:
//	        log.Printf("resource %d dropped", event.Handle)
//	    }
//	})
//
// # WASI Integration
//
// For WASI implementations, use the predefined type IDs:
//
//	resource.WASIInputStream   // wasi:io/streams input-stream
//	resource.WASIOutputStream  // wasi:io/streams output-stream
//	resource.WASIPollable      // wasi:io/poll pollable
//	resource.WASIDescriptor    // wasi:filesystem/types descriptor
//
// # Memory Management
//
// Resources are not automatically garbage collected. The host must explicitly
// call Remove() or Drop() when the WASM component drops a resource handle.
// Failure to do so will leak memory.
//
// For pooled instances, call table.Close() to release all resources when
// the instance is recycled.
package resource
