main: main.docs-content
block: none

# Reading files

The SDK reads local and remote files through one call, `sdk.open(path)`, which returns a `File`.

## Open a file

```
import sdk

with sdk.open("data/report.csv") as f:
    for row in f.rows():
        print(row)
```

## Stream a file

Large files stream in chunks. Pass a chunk size:

- Local files
  - read with `mmap` when possible
  - fall back to buffered reads
- Remote files, over HTTPS only

| Parameter | Type | Default |
|---|---|---|
| `chunk_size` | int | 65536 |
| `timeout` | float | 30.0 |

**File**

: An open file. Close it, or use it in a `with` block.

See [Errors](/docs/errors) for what `open` raises, and the [HTTP semantics RFC](rfc-editor.org/rfc/rfc9110) for remote files.
