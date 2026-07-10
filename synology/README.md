# Synology DSM 7 package

The `package/` directory is a DSM 7 SPK layout. Release automation copies the
matching Linux binary to `package/package/bin/home-store` and runs
`build-spk.sh` to create an architecture-specific `.spk`.

The package runs as the lower-privilege package service user declared in
`conf/privilege`. On first start it creates configuration under
`/var/packages/home-store/var/config.env`. Set the access key, secret key,
port, and durable shared-folder paths there before exposing the service.

Validate every published SPK on real DSM 7 hardware or a DSM VM. The repository
can build the archive and validate its structure, but cannot emulate DSM's
package lifecycle or privilege system.
