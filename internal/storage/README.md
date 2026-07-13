# Storage package

The filesystem backend stores immutable opaque blobs under the configured data
directory and keeps bucket, object, and multipart metadata in SQLite. SQLite is
the publication point: a row references only a fully written and synced blob.
Replaced/deleted blobs are removed after the metadata transaction commits, and
startup removes unreferenced blobs left by interrupted work.

The backend enforces a single process per database, serializes conflicting
mutations, evaluates write conditions at commit time, and migrates the earlier
JSON metadata/direct-key layout. Object keys are database values and never
filesystem paths.

`storage.Backend` covers bucket and object operations, conditional puts, and
paginated listings. `storage.MultipartBackend` covers persistent multipart
uploads and parts.

Both the data directory and SQLite file are required to restore a store.
