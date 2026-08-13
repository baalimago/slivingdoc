# slivingdoc (npm launcher)

This package is the npm installation path for the slivingdoc MCP notebook
server (architecture section 21). The package itself contains no native
code. `slivingdoc` selects the binary for its exact version and platform
and downloads it from the matching GitHub release. It verifies the
published SHA-256 and caches the verified bytes under the npm cache. It
then executes the binary with your arguments and standard streams.

Requirements: Node.js 24 or newer and one of the supported targets
(`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`,
`windows/amd64`). An unsupported platform fails before any download with
an actionable error.

An MCP host starts the server as a child process, or you can run it directly:

```text
npx -y slivingdoc version
npx -y slivingdoc serve --bucket my-notes --workspace-root /srv/notes ...
```

The launcher never writes to stdout: stdout belongs to the MCP protocol
and to the child process. All launcher diagnostics go to stderr. Arguments
are forwarded verbatim, and stdin stays connected. The child's exit status
(or terminating signal) becomes the launcher's.

## Cache

Downloads live under the npm cache at a
`version/os/architecture/asset`-specific path and are verified again on
every run. A corrupt or partial download is deleted and re-fetched. It is
never executed. The launcher retries transient network failures with
exponential backoff. It reports a missing asset immediately.
`SLIVINGDOC_CACHE` relocates the cache, and `SLIVINGDOC_RELEASE_BASE`
points the launcher at a mirror of the GitHub release download URLs (tests
use both).

## Publication gate

`npm run check-release` (wired into `prepublishOnly`) fails until the
GitHub release for the package version contains every required artifact,
the `SHA256SUMS` checksum file, and the license `NOTICE`. Thus npm
publication can never precede the complete GitHub release.

On a tagged release, the repository workflow publishes this package
automatically after the GitHub release is complete. It fails unless the
package version matches the tag. It publishes prerelease versions to the
`next` dist-tag and stable versions to `latest`. Publication uses npm
trusted publishing (OIDC). No npm token is stored in the repository.
