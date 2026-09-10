package engine

import "github.com/tetratelabs/wazero/api"

// LinearMemoryUsage reports all distinct core linear memories, independently of
// the memory selected by an export's canonical ABI. The boolean distinguishes a
// zero-page memory from an instance with no memory. It excludes compiled code
// and host resources. Callers must serialize this with instance Close.
func (i *WazeroInstance) LinearMemoryUsage() (bytes uint64, present bool) {
	if i == nil {
		return 0, false
	}
	if i.linkerInst != nil {
		return coreMemoryUsage(i.linkerInst.Modules())
	}
	if i.instance == nil {
		return 0, false
	}
	return coreMemoryUsage([]api.Module{i.instance})
}

func coreMemoryUsage(modules []api.Module) (bytes uint64, present bool) {
	for idx, mod := range modules {
		if mod == nil {
			continue
		}
		mem := mod.Memory()
		if !hasMemory(mem) {
			continue
		}
		present = true
		shared := false
		for _, prev := range modules[:idx] {
			if prev != nil && prev.Memory() == mem {
				shared = true
				break
			}
		}
		if shared {
			continue
		}
		// Size is uint32 and wraps at 4 GiB. Grow(0) queries the page count
		// without changing memory and represents the full wasm32 range.
		pages, ok := mem.Grow(0)
		if ok {
			bytes += uint64(pages) * 65536
		} else {
			bytes += uint64(mem.Size())
		}
	}
	return bytes, present
}
