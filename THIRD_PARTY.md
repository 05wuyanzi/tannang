# Third-party material

Current repository state:

```yaml
third_party_source_included: false
third_party_binary_included: false
third_party_binary_executed: false
external_backend_executed: false
third_party_go_modules: false
```

The first genesis uses only the Go standard library. No optional external
backend is bundled, downloaded, linked, or invoked.

Future external backends, if accepted, will remain user-supplied and
process-isolated. Each adapter will require an explicit provider descriptor,
path/version/hash validation, bounded execution, output re-hashing, and a
separate license review.

This file does not grant trust or execution authority to any future backend.
