"""Reproduce the structural annotation fixture; it has unresolved export indices.

Grammar: WebAssembly/component-model Binary.md at
8892da0f5587c4253bb82b9b06120b9a8eef2b26, nameattributes/attribute/export.
This tests decoding, not component validation or execution.
"""
from pathlib import Path


def u32(value):
    out = bytearray()
    while True:
        byte = value & 127
        value >>= 7
        out.append(byte | (128 if value else 0))
        if not value:
            return bytes(out)


def string(value):
    data = value.encode("utf-8")
    return u32(len(data)) + data


def section(kind, data):
    return bytes([kind]) + u32(len(data)) + data


exports = u32(4)
exports += bytes([2]) + string("primary") + u32(2)
exports += bytes([0]) + string("wasi:keyvalue/store")
exports += bytes([2]) + string("https://example.com/kv")
exports += bytes([5, 0, 0])  # instance index 0, absent type ascription
exports += bytes([2]) + string("wasi:cli/run@0.2") + u32(1)
exports += bytes([1]) + string(".0") + bytes([1, 0, 0])
exports += bytes([1]) + string("legacy") + bytes([1, 0, 0])
exports += bytes([0]) + string("plain") + bytes([1, 0, 0])
binary = b"\0asm\x0d\0\x01\0" + section(1, b"\0asm\x01\0\0\0") + section(11, exports)
Path(__file__).with_name("packed_exports_annotations.wasm").write_bytes(binary)
