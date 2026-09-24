# Conformance Suites

A conformance suite is one set of tests that every adapter of a port must pass, so protocols behave the same for the rest of the system.

## RemoteFS

`internal/adapters/remotefs/remotefstest.Run(t, factory)` exercises listing, stat, directories, reads and writes (create, overwrite, resume), removal, renames, permissions, times, canceled contexts and use after close.

Adding an adapter:

1. Write a test that calls `remotefstest.Run` with a factory returning a fresh, empty file system.
2. Advertise optional features in `Capabilities()`; the suite skips or checks them accordingly.
3. Wrap the sentinel errors of `internal/core/remotefs` with `%w`; the suite uses `errors.Is`.

## Keep the suite honest

Passing is not enough: check that the suite fails when the adapter is wrong. Break the reference adapter on purpose (allow renames over existing files, delete non-empty directories, return local times) and confirm a test fails. A mutation that survives is a gap in the suite; add the assertion.

## Reference adapter

`memfs` is an in-memory implementation that passes the suite and also serves as a test double for the transfer queue.
