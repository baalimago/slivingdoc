#!/usr/bin/env node
// bin/slivingdoc.mjs — the npm launcher entry point.
//
// The launcher resolves the native slivingdoc binary for the current
// platform, downloads and verifies it from the matching GitHub release when
// no verified copy exists in the cache, and then executes it with the
// caller's arguments and standard streams. The launcher never writes to
// stdout: stdout belongs to the MCP protocol and to the child process.
import { run } from "../lib/launcher.mjs";

run().catch((err) => {
	console.error(`slivingdoc: ${err.message}`);
	process.exit(1);
});
