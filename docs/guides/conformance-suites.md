# Conformance Suites

A conformance suite is one set of tests that every adapter of a port must pass, so protocols behave the same for the rest of the system.

## RemoteFS

`internal/adapters/remotefs/remotefstest.Run(t, factory)` exercises listing, stat, directories, reads and writes (create, overwrite, resume), removal, renames, permissions, times, canceled contexts and use after close.

Adding an adapter:

1. Write a test that calls `remotefstest.Run` with a factory returning a fresh, empty file system.
2. Advertise optional features in `Capabilities()`; the suite skips or checks them accordingly.
3. Wrap the sentinel errors of `internal/core/remotefs` with `%w`; the suite uses `errors.Is`.

## Connectors

`internal/adapters/remotefs/connectortest.Run(t, descriptor, factory)` is the suite every connector of the catalog passes (ADR-0019). On top of `remotefstest.Run` it checks that the descriptor is valid and that the capabilities it declares are the ones its adapter reports (`Mismatches`): permissions, times that can be set and resumable writes must be declared exactly when the adapter has them, because the interface offers features by the declaration. Real folders and renames are required until `remotefstest` learns to check connectors without them, and server-side copy cannot be declared until the port has an operation for it.

`TestConformance_Catalog` in `internal/adapters/remotefs/connect` runs it for every built-in connector, reached the way the explorer reaches it: a site saved through the schema, the connector's opener and its adapter, on a local reference server that supports everything the protocol can do (an in-process SFTP server, ftpserverlib with FTPS, gofakes3 for S3). Adding a connector:

1. Register its descriptor and its opener.
2. Add its reference server to `references` in that test; a connector without one fails the test.
3. Declare only the capabilities its adapter reports on that server.

## Keep the suite honest

Passing is not enough: check that the suite fails when the adapter is wrong. Break the reference adapter on purpose (allow renames over existing files, delete non-empty directories, return local times) and confirm a test fails. A mutation that survives is a gap in the suite; add the assertion.

## Concurrency needs a deterministic test

A statistical race test (many goroutines racing) can pass even when the lock is missing, because the race window is microseconds. Prove locking deterministically: hold the lock from another connection, start the operation, assert it blocks, release the lock and assert it completes. The first-user creation test does this with the Postgres advisory lock, and it fails when the lock is removed.

## Reference adapter

`memfs` is an in-memory implementation that passes the suite and also serves as a test double for the transfer queue.
